package provapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream"
	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream/downstreamtest"
)

func TestStatus(t *testing.T) {
	srv := downstreamtest.NewServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/v1/status" {
			http.NotFound(w, r)
			return
		}
		downstreamtest.WriteJSON(w, 200, map[string]any{
			"version": "0.3.0", "nodeName": "poc-01", "startedAt": "2026-10-09T00:00:00Z",
			"subscriberCount": 3, "clientCount": 2, "policyCount": 4, "sessionCount": 1,
		})
	})
	c, err := New(srv.Options)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != downstream.Prov {
		t.Errorf("name = %s", c.Name())
	}
	st, err := c.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "0.3.0" || st.NodeName != "poc-01" || st.SubscriberCount != 3 || st.PolicyCount != 4 ||
		st.SessionCount == nil || *st.SessionCount != 1 {
		t.Errorf("status = %+v", st)
	}
}

func TestDiagnose(t *testing.T) {
	// 文面が provisioning-api の設定と手順を指していること（判定そのものは internal/downstream で確かめる）。
	dnsErr := fmt.Errorf("wrap: %w", &net.DNSError{Name: "provisioning-api", IsNotFound: true})
	if got := Diagnose(dnsErr); !strings.Contains(got, "（provisioning-api）") || !strings.Contains(got, "eapaka-prov") {
		t.Errorf("dns: %q", got)
	}
	for name, h := range map[string]string{
		"unknown server":  hints.UnknownServer,
		"client rejected": hints.ClientRejected,
		"reset":           hints.ConnectionReset,
	} {
		if !strings.Contains(h, "PROVISIONER_PROV_SERVER_CERT") && !strings.Contains(h, "PROVISIONING_API_ADMIN_CLIENTS") {
			t.Errorf("%s: %q", name, h)
		}
	}
}
