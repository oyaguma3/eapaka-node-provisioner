package store

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

// openTestStore は、接続先を環境変数で指定したときだけ実際の Valkey に接続する。
// 論理データベース 1 番を使う（テストで全データを消すことがある）。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	addr := os.Getenv("PROVISIONER_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("PROVISIONER_TEST_VALKEY_ADDR is not set")
	}
	s, err := Open(t.Context(), Options{Addr: addr, Password: os.Getenv("PROVISIONER_TEST_VALKEY_PASSWORD"), DB: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.c.Do(t.Context(), s.c.B().Flushdb().Build()).Error(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPing(t *testing.T) {
	s := openTestStore(t)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAudit(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	if got, next, err := s.ListAudit(ctx, "", 10); err != nil || len(got) != 0 || next != "" {
		t.Errorf("empty: %v, %q, %v", got, next, err)
	}
	for i := range 5 {
		e := AuditEntry{Operator: "alice", MgmtClient: "bff", Action: "policy.create", Target: fmt.Sprint(i),
			TraceID: "t" + fmt.Sprint(i), Result: "completed", Details: `{"downstream":"prov"}`}
		if i == 4 {
			e.OperationID = "op-1"
		}
		if err := s.AppendAudit(ctx, e, 1000); err != nil {
			t.Fatal(err)
		}
	}
	// 新しい順に返り、nextBefore で続きをたどれる。
	var targets []string
	before := ""
	for {
		got, next, err := s.ListAudit(ctx, before, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range got {
			if e.Operator != "alice" || e.MgmtClient != "bff" || e.Action != "policy.create" || e.Result != "completed" ||
				e.TraceID != "t"+e.Target || e.Details != `{"downstream":"prov"}` || time.Since(e.Time) > time.Minute || e.ID == "" {
				t.Errorf("entry = %+v", e)
			}
			if (e.Target == "4") != (e.OperationID == "op-1") {
				t.Errorf("operation id = %+v", e)
			}
			targets = append(targets, e.Target)
		}
		if next == "" {
			break
		}
		before = next
	}
	if want := []string{"4", "3", "2", "1", "0"}; !slices.Equal(targets, want) {
		t.Errorf("targets = %v, want %v", targets, want)
	}
}

func TestIMSILock(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	const imsi = "001010000000001"
	token, err := s.AcquireIMSILock(ctx, imsi, time.Minute)
	if err != nil || token == "" {
		t.Fatalf("acquire: %q, %v", token, err)
	}
	if _, err := s.AcquireIMSILock(ctx, imsi, time.Minute); !errors.Is(err, ErrLocked) {
		t.Errorf("second acquire: %v", err)
	}
	// 別の IMSI は取れる。
	if _, err := s.AcquireIMSILock(ctx, "001010000000002", time.Minute); err != nil {
		t.Errorf("other imsi: %v", err)
	}
	// 違うトークンでは解放されない。
	if err := s.ReleaseIMSILock(ctx, imsi, "other"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireIMSILock(ctx, imsi, time.Minute); !errors.Is(err, ErrLocked) {
		t.Errorf("after wrong release: %v", err)
	}
	if err := s.ReleaseIMSILock(ctx, imsi, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcquireIMSILock(ctx, imsi, 50*time.Millisecond); err != nil {
		t.Errorf("after release: %v", err)
	}
	// 有効期限が過ぎると自然に外れる。
	time.Sleep(100 * time.Millisecond)
	if _, err := s.AcquireIMSILock(ctx, imsi, time.Minute); err != nil {
		t.Errorf("after expiry: %v", err)
	}
}

func TestIdempotency(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	begin := func(client, key, hash string) (IdempotencyState, StoredResponse) {
		t.Helper()
		st, resp, err := s.BeginIdempotent(ctx, client, key, hash, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return st, resp
	}

	if st, _ := begin("bff", "k1", "h1"); st != IdempotencyNew {
		t.Fatalf("first: %v", st)
	}
	if st, _ := begin("bff", "k1", "h1"); st != IdempotencyInProgress {
		t.Errorf("while pending: %v", st)
	}
	if st, _ := begin("bff", "k1", "h2"); st != IdempotencyMismatch {
		t.Errorf("other request: %v", st)
	}
	// 管理クライアントが違えば別のキー。
	if st, _ := begin("other", "k1", "h2"); st != IdempotencyNew {
		t.Errorf("other client: %v", st)
	}

	want := StoredResponse{Status: 201, ContentType: "application/json", Location: "/admin/v1/clients/7", Body: []byte(`{"id":7}`)}
	if err := s.CompleteIdempotent(ctx, "bff", "k1", "h1", want, time.Hour); err != nil {
		t.Fatal(err)
	}
	if st, got := begin("bff", "k1", "h1"); st != IdempotencyReplay || got.Status != 201 || got.ContentType != want.ContentType ||
		got.Location != want.Location || string(got.Body) != string(want.Body) {
		t.Errorf("replay: %v, %+v", st, got)
	}
	if st, _ := begin("bff", "k1", "h2"); st != IdempotencyMismatch {
		t.Errorf("other request after done: %v", st)
	}
	// 完了した記録は、内容の違う Complete / Abandon では変わらない。
	if err := s.AbandonIdempotent(ctx, "bff", "k1", "h1"); err != nil {
		t.Fatal(err)
	}
	if st, _ := begin("bff", "k1", "h1"); st != IdempotencyReplay {
		t.Errorf("abandon after done: %v", st)
	}

	// 処理中を取り消すと、同じキーでやり直せる。
	if st, _ := begin("bff", "k2", "h1"); st != IdempotencyNew {
		t.Fatal(st)
	}
	if err := s.AbandonIdempotent(ctx, "bff", "k2", "h1"); err != nil {
		t.Fatal(err)
	}
	if st, _ := begin("bff", "k2", "h1"); st != IdempotencyNew {
		t.Errorf("after abandon: %v", st)
	}

	// 処理中の記録は pendingTTL で消える（プロセスが落ちても、やり直せる）。
	if st, _, err := s.BeginIdempotent(ctx, "bff", "k3", "h1", 50*time.Millisecond); err != nil || st != IdempotencyNew {
		t.Fatal(st, err)
	}
	time.Sleep(100 * time.Millisecond)
	if st, _ := begin("bff", "k3", "h1"); st != IdempotencyNew {
		t.Errorf("after pending ttl: %v", st)
	}
	// 本文は空でも覚えられる（204 の応答）。
	if err := s.CompleteIdempotent(ctx, "bff", "k3", "h1", StoredResponse{Status: 204}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if st, got := begin("bff", "k3", "h1"); st != IdempotencyReplay || got.Status != 204 || len(got.Body) != 0 {
		t.Errorf("204 replay: %v, %+v", st, got)
	}
}

func TestOperation(t *testing.T) {
	s := openTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	op := Operation{
		ID: "op-1", Kind: "subscriber.update", IMSI: "001010000000001", KeyStore: "poc", Status: OpRunning,
		Steps: []OperationStep{
			{Name: "policy.put", Downstream: "prov", State: StepDone},
			{Name: "subscriber.update", Downstream: "prov", State: StepFailed,
				Error: &StepError{Status: 503, Cause: "DOWNSTREAM_UNAVAILABLE", Detail: "timeout", Time: now}},
		},
		NextAttemptAt: now.Add(time.Minute), Operator: "alice", MgmtClient: "bff", TraceID: "t1",
		CreatedAt: now, UpdatedAt: now, PrevPolicy: `{"default":"deny","rules":[]}`, HadPolicy: true,
	}
	if err := s.SaveOperation(ctx, op, time.Hour); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOperation(ctx, "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != op.Kind || got.IMSI != op.IMSI || got.Status != OpRunning || len(got.Steps) != 2 ||
		got.Steps[1].Error == nil || got.Steps[1].Error.Status != 503 || !got.Steps[1].Error.Time.Equal(now) ||
		!got.NextAttemptAt.Equal(op.NextAttemptAt) || !got.CreatedAt.Equal(now) || got.PrevPolicy != op.PrevPolicy || !got.HadPolicy ||
		got.Operator != "alice" || got.MgmtClient != "bff" || got.TraceID != "t1" || got.Final() {
		t.Errorf("got = %+v", got)
	}
	// 未完了なら ops:active に入り、期限はない。
	if score, err := s.c.Do(ctx, s.c.B().Zscore().Key(keyActiveOps).Member("op-1").Build()).AsFloat64(); err != nil ||
		int64(score) != op.NextAttemptAt.UnixMilli() {
		t.Errorf("active score = %v, %v", score, err)
	}
	if ttl, _ := s.c.Do(ctx, s.c.B().Pttl().Key(opKey("op-1")).Build()).AsInt64(); ttl != -1 {
		t.Errorf("ttl while running = %d", ttl)
	}

	// 完了したら ops:active から外れ、retention の後に消える。
	op.Status, op.Attempts = OpRolledBack, 1
	if err := s.SaveOperation(ctx, op, time.Hour); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.c.Do(ctx, s.c.B().Zcard().Key(keyActiveOps).Build()).AsInt64(); n != 0 {
		t.Errorf("active after final = %d", n)
	}
	if ttl, _ := s.c.Do(ctx, s.c.B().Pttl().Key(opKey("op-1")).Build()).AsInt64(); ttl <= 0 || ttl > time.Hour.Milliseconds() {
		t.Errorf("ttl after final = %d", ttl)
	}
	if got, err := s.GetOperation(ctx, "op-1"); err != nil || got.Status != OpRolledBack || got.Attempts != 1 || !got.Final() {
		t.Errorf("final = %+v, %v", got, err)
	}
	if _, err := s.GetOperation(ctx, "none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}
