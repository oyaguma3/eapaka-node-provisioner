package store

import (
	"os"
	"testing"
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
