package downstream

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
)

// Hints は、下流に接続できなかったときに示す対処の文。下流ごとに設定の名前や手順が違うので、
// internal/provapi と internal/akaapi で用意する。
type Hints struct {
	// HostnameMismatch は、下流のサーバー証明書の SAN に接続先が入っていないとき。
	HostnameMismatch string
	// UnknownServer は、下流のサーバー証明書が設定した証明書と一致しないとき。
	UnknownServer string
	// ServerCertInvalid は、下流のサーバー証明書が有効期間外のとき。
	ServerCertInvalid string
	// ClientRejected は、下流が provisioner のクライアント証明書を受け付けなかったとき。
	ClientRejected string
	// ConnectionReset は、下流が接続を切ったとき（クライアント証明書の不受理の可能性がある）。
	ConnectionReset string
	// NameNotFound は、下流のホスト名を解決できないとき。%s にホスト名が入る。
	NameNotFound string
	// DialFailed は、下流に接続できないとき。
	DialFailed string
	// Timeout は、下流から時間内に応答がないとき。
	Timeout string
}

// Diagnose は、下流に接続できなかったときの原因の見当を、運用者向けの文で返す。
// 見当がつかなければ空文字列を返す。
func Diagnose(err error, h Hints) string {
	if err == nil {
		return ""
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return h.HostnameMismatch
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return h.UnknownServer
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return h.ServerCertInvalid
	}
	// 相手から TLS のアラートを受け取ると、Op が "remote error" の *net.OpError になる。
	// 下流は未登録・有効期間外・証明書なしの接続を bad_certificate 等で拒否する。
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "remote error" &&
		(strings.Contains(opErr.Err.Error(), "bad certificate") || strings.Contains(opErr.Err.Error(), "certificate required")) {
		return h.ClientRejected
	}
	// TLS 1.3 では、下流がクライアント証明書を検証して拒否する前に、provisioner はハンドシェイクを終えて
	// リクエストを送っている。下流が読まずに接続を閉じると、拒否のアラートより先に接続のリセットが届き、
	// アラートを受け取れないことがある（web-gui-for-eapaka-radius の手元の確認で 16 回に 1 回ほど）。
	if errors.Is(err, syscall.ECONNRESET) {
		return h.ConnectionReset
	}
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr.IsNotFound {
		// 同一ホストの構成では、下流のコンテナが止まっているときも名前を解決できなくなる。
		return fmt.Sprintf(h.NameNotFound, dnsErr.Name)
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
		return h.DialFailed
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return h.Timeout
	}
	return ""
}
