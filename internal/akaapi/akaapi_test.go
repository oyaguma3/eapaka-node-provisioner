package akaapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream"
	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream/downstreamtest"
)

func TestStatusAndAVClient(t *testing.T) {
	srv := downstreamtest.NewServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v1/status":
			downstreamtest.WriteJSON(w, 200, map[string]any{
				"version": "1.0.0", "bootId": "b", "startedAt": "2026-10-09T00:00:00Z",
				"subscriberCount": 7, "clientCount": 2, "avPlainEnabled": false,
				"avServerCertificateNotAfter": "2036-10-01T00:00:00Z",
			})
		case "/admin/v1/clients/1":
			downstreamtest.WriteJSON(w, 200, map[string]any{
				"id": 1, "name": "vector-gateway", "enabled": true, "networkName": "",
				"notAfter": "2028-10-01T00:00:00Z",
			})
		default:
			downstreamtest.WriteProblem(w, 404, `{"title":"Not Found","status":404,"cause":"CLIENT_NOT_FOUND"}`)
		}
	})
	c, err := New(srv.Options)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != downstream.Aka {
		t.Errorf("name = %s", c.Name())
	}
	st, err := c.Status(t.Context())
	if err != nil || st.Version != "1.0.0" || st.SubscriberCount != 7 {
		t.Errorf("status = %+v, %v", st, err)
	}
	av, err := c.GetAVClient(t.Context(), 1)
	if err != nil || av.ID != 1 || av.Name != "vector-gateway" || !av.Enabled {
		t.Errorf("av client = %+v, %v", av, err)
	}
	_, err = c.GetAVClient(t.Context(), 9)
	if apiErr, ok := errors.AsType[*downstream.Error](err); !ok || apiErr.Status != 404 || downstream.CauseOf(err) != CauseClientNotFound {
		t.Errorf("missing av client: %v", err)
	}
}

func TestDiagnose(t *testing.T) {
	// 文面が aka-only-server の設定と手順を指していること（判定そのものは internal/downstream で確かめる）。
	dnsErr := fmt.Errorf("wrap: %w", &net.DNSError{Name: "aka-only-server", IsNotFound: true})
	if got := Diagnose(dnsErr); !strings.Contains(got, "（aka-only-server）") || !strings.Contains(got, "aka-av") {
		t.Errorf("dns: %q", got)
	}
	if !strings.Contains(hints.UnknownServer, "PROVISIONER_AKA_SERVER_CERT") || !strings.Contains(hints.ClientRejected, "AKA_ADMIN_CLIENTS") {
		t.Errorf("hints = %+v", hints)
	}
}
