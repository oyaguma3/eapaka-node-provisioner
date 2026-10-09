package certs

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestEnsureFilesGenerates(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls", "cert.pem")
	keyPath := filepath.Join(dir, "tls", "key.pem")

	created, err := EnsureFiles(certPath, keyPath, []string{"gui.example.ts.net", "100.64.0.1"})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	ki, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if ki.Mode().Perm() != 0o600 {
		t.Errorf("key perm = %v", ki.Mode().Perm())
	}

	f, err := LoadFile(certPath, keyPath, discard)
	if err != nil {
		t.Fatal(err)
	}
	leaf := f.Leaf()
	if !slices.Equal(leaf.DNSNames, []string{"gui.example.ts.net"}) {
		t.Errorf("DNSNames = %v", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(net.ParseIP("100.64.0.1")) {
		t.Errorf("IPAddresses = %v", leaf.IPAddresses)
	}

	// 2 回目は生成せず、既存のものを使う。
	created, err = EnsureFiles(certPath, keyPath, nil)
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	f2, err := LoadFile(certPath, keyPath, discard)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(f2.Leaf()) != Fingerprint(leaf) {
		t.Error("certificate changed on second call")
	}
}

func TestEnsureFilesPartial(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFiles(certPath, keyPath, nil); err == nil {
		t.Error("only cert exists: want error")
	}
	if _, err := os.Stat(keyPath); err == nil {
		t.Error("key must not be generated")
	}
}

func writePair(t *testing.T, certPath, keyPath string, host string) {
	t.Helper()
	certPEM, keyPEM, err := SelfSigned("test", []string{host}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileReload(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	writePair(t, certPath, keyPath, "old.example")

	f, err := LoadFile(certPath, keyPath, discard)
	if err != nil {
		t.Fatal(err)
	}
	old := Fingerprint(f.Leaf())

	// 変更がなければそのまま。
	f.reloadIfChanged()
	if Fingerprint(f.Leaf()) != old {
		t.Fatal("reloaded without change")
	}

	// 秘密鍵が対応しない途中の状態では、それまでの証明書を使い続ける。
	certPEM, _, err := SelfSigned("test", []string{"new.example"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	f.reloadIfChanged()
	if Fingerprint(f.Leaf()) != old {
		t.Fatal("mismatched pair must not be loaded")
	}

	// 両方そろえば読み直す。
	writePair(t, certPath, keyPath, "new.example")
	f.reloadIfChanged()
	if got := f.Leaf().DNSNames; !slices.Equal(got, []string{"new.example"}) {
		t.Errorf("after reload DNSNames = %v", got)
	}

	// GetCertificate は確認間隔が過ぎるまでファイルを見ない。
	writePair(t, certPath, keyPath, "newer.example")
	cert, err := f.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cert.Leaf.DNSNames, []string{"new.example"}) {
		t.Errorf("GetCertificate before interval: DNSNames = %v", cert.Leaf.DNSNames)
	}
	f.mu.Lock()
	f.checkedAt = time.Now().Add(-checkInterval)
	f.mu.Unlock()
	if cert, _ = f.GetCertificate(nil); !slices.Equal(cert.Leaf.DNSNames, []string{"newer.example"}) {
		t.Errorf("GetCertificate after interval: DNSNames = %v", cert.Leaf.DNSNames)
	}
}

func TestSelfSignedClient(t *testing.T) {
	certPEM, keyPEM, err := SelfSignedClient("bff-01", 825*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	leaf := cert.Leaf
	if leaf.Subject.CommonName != "bff-01" || len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 0 {
		t.Errorf("subject = %v, SAN = %v %v", leaf.Subject, leaf.DNSNames, leaf.IPAddresses)
	}
	if !slices.Equal(leaf.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}) {
		t.Errorf("ExtKeyUsage = %v", leaf.ExtKeyUsage)
	}
	if d := leaf.NotAfter.Sub(leaf.NotBefore); d != 825*24*time.Hour {
		t.Errorf("validity = %v", d)
	}
}
