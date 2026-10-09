// Package akaapi は aka-only-server の管理API の呼び出し。
// API 仕様は aka-only-server のリポジトリの docs/openapi/admin-api.yaml を参照。接続の仕組みは internal/downstream。
package akaapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream"
)

// Client は aka-only-server の管理API のクライアント。
type Client struct {
	*downstream.Client
}

// New はクライアントを作る。opts.Name は設定しなくてよい。
func New(opts downstream.Options) (*Client, error) {
	opts.Name = downstream.Aka
	c, err := downstream.New(opts)
	if err != nil {
		return nil, err
	}
	return &Client{c}, nil
}

// Status は aka-only-server の状態。
type Status struct {
	Version                     string    `json:"version"`
	BootID                      string    `json:"bootId"`
	StartedAt                   time.Time `json:"startedAt"`
	SubscriberCount             int64     `json:"subscriberCount"`
	ClientCount                 int64     `json:"clientCount"`
	AVPlainEnabled              bool      `json:"avPlainEnabled"`
	AVServerCertificateNotAfter time.Time `json:"avServerCertificateNotAfter"`
}

// Status は aka-only-server の状態を取得する。
func (c *Client) Status(ctx context.Context) (Status, error) {
	return c.Call[Status](ctx, downstream.Request{Method: http.MethodGet, Path: []string{"status"}})
}

// AVClient は AVクライアント（認証ベクターAPI のクライアント）。
type AVClient struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	CertPEM           string    `json:"certPem"`
	FingerprintSHA256 string    `json:"fingerprintSha256"`
	Subject           string    `json:"subject"`
	NotBefore         time.Time `json:"notBefore"`
	NotAfter          time.Time `json:"notAfter"`
	Enabled           bool      `json:"enabled"`
	NetworkName       string    `json:"networkName"`
	CreatedAt         time.Time `json:"createdAt"`
}

// CauseClientNotFound は、AVクライアントが存在しないことを表す cause。
const CauseClientNotFound = "CLIENT_NOT_FOUND"

// GetAVClient は AVクライアントを取得する。
func (c *Client) GetAVClient(ctx context.Context, id int64) (AVClient, error) {
	return c.Call[AVClient](ctx, downstream.Request{
		Method: http.MethodGet, Path: []string{"clients", strconv.FormatInt(id, 10)},
	})
}

// hints は aka-only-server の管理API に接続できなかったときの対処の文。
var hints = downstream.Hints{
	HostnameMismatch: "aka-only-server の管理API のサーバー証明書に、接続先のホスト名（または IP アドレス）が入っていません。" +
		"aka-only-server 側の AKA_ADMIN_TLS_HOSTS に接続先（同一ホストなら aka-only-server）を加えて admin-cert reset で作り直し、" +
		"provisioner にも渡し直すか、証明書に入っている名前で接続してください。",
	UnknownServer: "aka-only-server の管理API のサーバー証明書が、設定した証明書（PROVISIONER_AKA_SERVER_CERT）と一致しません。" +
		"aka-only-server の admin-cert で取り出した証明書を設定してください。",
	ServerCertInvalid: "aka-only-server の管理API のサーバー証明書が有効期間外です。aka-only-server 側で admin-cert reset で作り直し、" +
		"provisioner にも渡し直してください。",
	ClientRejected: "aka-only-server が provisioner のクライアント証明書を受け付けませんでした。" +
		"aka-only-server 側の .env の AKA_ADMIN_CLIENTS に、このクライアント証明書のフィンガープリントが登録されているか" +
		"（登録後に aka-only-server を起動し直したか）、証明書が有効期間内かを確認してください。",
	ConnectionReset: "aka-only-server が接続を切りました。provisioner のクライアント証明書が受け付けられなかった可能性があります。" +
		"aka-only-server 側の .env の AKA_ADMIN_CLIENTS に、このクライアント証明書のフィンガープリントが登録されているか" +
		"（登録後に aka-only-server を起動し直したか）、証明書が有効期間内かを確認してください" +
		"（aka-only-server のログに admin client certificate rejected が出ていれば、これが原因です）。",
	NameNotFound: "aka-only-server のホスト名（%s）を解決できません。aka-only-server が起動しているか、" +
		"同一ホストの場合は provisioner が共有ネットワーク（aka-av）に参加しているか確認してください。",
	DialFailed: "aka-only-server の管理API に接続できません。aka-only-server が起動しているか（管理API は AKA_ADMIN_CLIENTS を" +
		"設定したときだけ有効）、接続先の URL とポートが正しいか確認してください。",
	Timeout: "aka-only-server の管理API から時間内に応答がありません。接続先のアドレス、VPN、aka-only-server 側の公開先（AKA_ADMIN_PUBLISH）を確認してください。",
}

// Diagnose は、aka-only-server の管理API に接続できなかったときの原因の見当を返す。見当がつかなければ空文字列。
func Diagnose(err error) string { return downstream.Diagnose(err, hints) }
