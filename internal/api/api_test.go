package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/oyaguma3/eapaka-node-provisioner/internal/akaapi"
	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream"
	"github.com/oyaguma3/eapaka-node-provisioner/internal/plmn"
	"github.com/oyaguma3/eapaka-node-provisioner/internal/provapi"
)

// ---- 偽物 ----

type fakeProv struct {
	status provapi.Status
	err    error
	// operators は呼び出しのコンテキストに入っていた操作者。
	operators []string
}

func (f *fakeProv) BaseURL() string { return "https://provisioning-api:9444/admin/v1" }
func (f *fakeProv) Status(ctx context.Context) (provapi.Status, error) {
	f.operators = append(f.operators, downstream.OperatorFrom(ctx))
	return f.status, f.err
}

type fakeAka struct {
	status    akaapi.Status
	statusErr error
	av        akaapi.AVClient
	avErr     error
}

func (f *fakeAka) BaseURL() string { return "https://aka-only-server:9443/admin/v1" }
func (f *fakeAka) Status(context.Context) (akaapi.Status, error) {
	return f.status, f.statusErr
}
func (f *fakeAka) GetAVClient(_ context.Context, id int64) (akaapi.AVClient, error) {
	return f.av, f.avErr
}

type fakeStore struct{ err error }

func (f *fakeStore) Ping(context.Context) error { return f.err }

type env struct {
	h    http.Handler
	prov *fakeProv
	aka  *fakeAka
	st   *fakeStore
	logs *bytes.Buffer
}

func newEnv(t *testing.T, withAka bool) *env {
	t.Helper()
	m, err := plmn.Parse("00102:01,001010:00")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		prov: &fakeProv{status: provapi.Status{Version: "0.3.0", NodeName: "poc-01", SubscriberCount: 3}},
		aka: &fakeAka{
			status: akaapi.Status{Version: "1.0.0", SubscriberCount: 7},
			av:     akaapi.AVClient{ID: 2, Name: "vector-gateway", Enabled: true},
		},
		st:   &fakeStore{},
		logs: &bytes.Buffer{},
	}
	h := &Handler{
		Log:               slog.New(slog.NewJSONHandler(e.logs, nil)),
		MgmtClient:        func(*http.Request) string { return "bff-01" },
		Prov:              e.prov,
		PLMNMap:           m,
		Store:             e.st,
		DownstreamTimeout: time.Second,
		Version:           "test",
		StartedAt:         time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
	}
	if withAka {
		h.Aka, h.AkaAVClientID = e.aka, 2
	}
	e.h = h.Routes()
	return e
}

func (e *env) do(method, path string, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://provisioner"+path, nil)
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, wantStatus, w.Body)
	}
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return v
}

// ---- /status ----

func TestStatus(t *testing.T) {
	e := newEnv(t, true)
	got := decode[map[string]any](t, e.do("GET", "/admin/v1/status", nil), http.StatusOK)
	want := map[string]any{
		"version":   "test",
		"startedAt": "2026-10-10T00:00:00Z",
		"plmnMap": []any{
			map[string]any{"plmn": "00102", "keyStore": "aka"},
			map[string]any{"plmn": "001010", "keyStore": "poc"},
		},
		"downstreams": map[string]any{
			"prov": map[string]any{"configured": true, "url": "https://provisioning-api:9444/admin/v1", "reachable": true,
				"version": "0.3.0", "nodeName": "poc-01", "subscriberCount": 3.0},
			"aka": map[string]any{"configured": true, "url": "https://aka-only-server:9443/admin/v1", "reachable": true,
				"version": "1.0.0", "subscriberCount": 7.0},
		},
		"avClient":   map[string]any{"id": 2.0, "exists": true, "enabled": true, "name": "vector-gateway"},
		"valkey":     map[string]any{"reachable": true},
		"operations": map[string]any{"running": 0.0, "retrying": 0.0, "failed": 0.0},
	}
	if !jsonEqual(got, want) {
		t.Errorf("status =\n%v\nwant\n%v", got, want)
	}
}

func TestStatusFailures(t *testing.T) {
	// 下流や Valkey に接続できなくても 200 で、項目ごとに示す。
	e := newEnv(t, true)
	e.prov.err = errors.New("dial tcp: connection refused")
	e.aka.avErr = &downstream.Error{Downstream: downstream.Aka, Status: 404, Problem: downstream.Problem{Cause: "CLIENT_NOT_FOUND"}}
	e.st.err = errors.New("valkey down")
	got := decode[statusJSON](t, e.do("GET", "/admin/v1/status", nil), http.StatusOK)
	if p := got.Downstreams.Prov; p.Reachable || p.Error == "" || p.SubscriberCount != nil {
		t.Errorf("prov = %+v", p)
	}
	if av := got.AVClient; av.Exists == nil || *av.Exists || av.Enabled != nil {
		t.Errorf("av client = %+v", av)
	}
	if got.Valkey.Reachable || got.Valkey.Error != "valkey down" || got.Operations != nil {
		t.Errorf("valkey = %+v, operations = %+v", got.Valkey, got.Operations)
	}

	// aka-only-server に接続できなければ、AVクライアントは分からない（exists を省略）。
	e = newEnv(t, true)
	e.aka.statusErr = errors.New("timeout")
	got = decode[statusJSON](t, e.do("GET", "/admin/v1/status", nil), http.StatusOK)
	if a := got.Downstreams.Aka; a.Reachable || a.Error != "timeout" || got.AVClient.Exists != nil || got.AVClient.ID != 2 {
		t.Errorf("aka = %+v, av = %+v", a, got.AVClient)
	}

	// aka-only-server を扱わない設定。
	e = newEnv(t, false)
	raw := decode[map[string]any](t, e.do("GET", "/admin/v1/status", nil), http.StatusOK)
	if !jsonEqual(raw["downstreams"].(map[string]any)["aka"], map[string]any{"configured": false, "reachable": false}) ||
		!jsonEqual(raw["avClient"], map[string]any{"id": 0.0}) {
		t.Errorf("aka not configured: %v / %v", raw["downstreams"], raw["avClient"])
	}
}

// ---- 共通の作法 ----

func TestTraceID(t *testing.T) {
	e := newEnv(t, false)
	if w := e.do("GET", "/admin/v1/status", map[string]string{"X-Trace-ID": "trace-001"}); w.Header().Get("X-Trace-ID") != "trace-001" {
		t.Errorf("given: %q", w.Header().Get("X-Trace-ID"))
	}
	generated := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, v := range []string{"", "has space"} {
		if w := e.do("GET", "/admin/v1/status", map[string]string{"X-Trace-ID": v}); !generated.MatchString(w.Header().Get("X-Trace-ID")) {
			t.Errorf("%q: %q", v, w.Header().Get("X-Trace-ID"))
		}
	}
	// エラーの応答にも付ける。
	if w := e.do("GET", "/admin/v1/nothing", map[string]string{"X-Trace-ID": "trace-404"}); w.Header().Get("X-Trace-ID") != "trace-404" {
		t.Errorf("404: %q", w.Header().Get("X-Trace-ID"))
	}
}

func TestOperator(t *testing.T) {
	e := newEnv(t, false)
	// 形式が違えば 400。
	p := decode[problem](t, e.do("GET", "/admin/v1/status", map[string]string{"X-Operator-Id": "bad operator!"}), http.StatusBadRequest)
	if p.Cause != "OPTIONAL_IE_INCORRECT" || len(p.InvalidParams) != 1 || p.InvalidParams[0].Param != "X-Operator-Id" {
		t.Errorf("problem = %+v", p)
	}
	// 下流にそのまま渡す（省略された場合は渡さない）。
	e.do("GET", "/admin/v1/status", map[string]string{"X-Operator-Id": "alice@example"})
	e.do("GET", "/admin/v1/status", nil)
	if len(e.prov.operators) != 2 || e.prov.operators[0] != "alice@example" || e.prov.operators[1] != "" {
		t.Errorf("operators = %q", e.prov.operators)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	e := newEnv(t, false)
	w := e.do("GET", "/admin/v1/nothing", nil)
	if p := decode[problem](t, w, http.StatusNotFound); p.Cause != "" || p.Title != "Not Found" ||
		w.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("404 = %+v", p)
	}
	w = e.do("DELETE", "/admin/v1/status", nil)
	if p := decode[problem](t, w, http.StatusMethodNotAllowed); p.Cause != "" || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("405 = %+v, Allow = %q", p, w.Header().Get("Allow"))
	}
}

func TestRequestLog(t *testing.T) {
	e := newEnv(t, false)
	e.do("GET", "/admin/v1/status?x=secret", map[string]string{"X-Trace-ID": "trace-log", "X-Operator-Id": "alice"})
	e.do("GET", "/admin/v1/status", map[string]string{"X-Trace-ID": "trace-bad", "X-Operator-Id": "bad operator!"})

	entries := map[string]map[string]any{}
	sc := bufio.NewScanner(e.logs)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err == nil && m["msg"] == "request completed" {
			entries[m["trace_id"].(string)] = m
		}
	}
	if m := entries["trace-log"]; m == nil || m["method"] != "GET" || m["path"] != "/admin/v1/status" ||
		m["http_status"] != 200.0 || m["mgmt_client"] != "bff-01" || m["operator"] != "alice" {
		t.Errorf("log = %v", m)
	}
	// 形式の違う操作者 ID はそのまま記録しない。
	if m := entries["trace-bad"]; m == nil || m["http_status"] != 400.0 || m["operator"] != "" {
		t.Errorf("bad operator log = %v", m)
	}
	// クエリ文字列は出さない。
	if bytes.Contains(e.logs.Bytes(), []byte("secret")) {
		t.Errorf("query string is logged: %s", e.logs)
	}
}

func jsonEqual(a, b any) bool {
	x, err1 := json.Marshal(a, json.Deterministic(true))
	y, err2 := json.Marshal(b, json.Deterministic(true))
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}
