package plmn

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	m, err := Parse(" 44010:01 , ,001010:00,00102:01,44010:00")
	if err != nil {
		t.Fatal(err)
	}
	// 同じ PLMN は後のものを使う（本PoCの vector-gateway と同じ）。
	want := []Entry{{"001010", KeyStorePoC}, {"00102", KeyStoreAKA}, {"44010", KeyStorePoC}}
	if got := m.Entries(); !slices.Equal(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
	if !m.Uses(KeyStoreAKA) || !m.Uses(KeyStorePoC) {
		t.Error("Uses")
	}

	empty, err := Parse("")
	if err != nil || len(empty.Entries()) != 0 || empty.Uses(KeyStoreAKA) {
		t.Errorf("empty: %v, %v", empty.Entries(), err)
	}

	for _, s := range []string{"44010", "4401:01", "4401000:01", "44a10:01", "44010:02", "44010:1", "44010:"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want error", s)
		}
	}
}

func TestKeyStore(t *testing.T) {
	m, err := Parse("00102:01,001030:01,00103:00")
	if err != nil {
		t.Fatal(err)
	}
	for imsi, want := range map[string]KeyStore{
		"001020000000001": KeyStoreAKA, // 5 桁で一致
		"001030000000001": KeyStoreAKA, // 6 桁が 5 桁より優先
		"001031000000001": KeyStorePoC, // 5 桁の 00
		"001010000000001": KeyStorePoC, // 一致なし
		"0010":            KeyStorePoC,
	} {
		if got := m.KeyStore(imsi); got != want {
			t.Errorf("KeyStore(%s) = %s, want %s", imsi, got, want)
		}
	}
	if got := (Map{}).KeyStore("001020000000001"); got != KeyStorePoC {
		t.Errorf("zero map = %s", got)
	}
}
