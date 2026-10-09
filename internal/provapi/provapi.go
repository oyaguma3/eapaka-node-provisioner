// Package provapi は本PoC（eapaka-radius-server-poc）の Provisioning API（provisioning-api）の呼び出し。
// API 仕様は本PoCのリポジトリの docs/openapi/provisioning-api.yaml を参照。接続の仕組みは internal/downstream。
package provapi

import (
	"context"
	"net/http"
	"time"

	"github.com/oyaguma3/eapaka-node-provisioner/internal/downstream"
)

// Client は Provisioning API のクライアント。
type Client struct {
	*downstream.Client
}

// New はクライアントを作る。opts.Name は設定しなくてよい。
func New(opts downstream.Options) (*Client, error) {
	opts.Name = downstream.Prov
	c, err := downstream.New(opts)
	if err != nil {
		return nil, err
	}
	return &Client{c}, nil
}

// Status は provisioning-api の状態。
type Status struct {
	Version string `json:"version"`
	// NodeName はノードの識別名（本PoC側の PROVISIONING_API_NODE_NAME）。
	NodeName        string    `json:"nodeName"`
	StartedAt       time.Time `json:"startedAt"`
	SubscriberCount int64     `json:"subscriberCount"`
	ClientCount     int64     `json:"clientCount"`
	PolicyCount     int64     `json:"policyCount"`
	// SessionCount はアクティブセッションの数。0.3.0 より前の provisioning-api は返さない（nil）。
	SessionCount *int64 `json:"sessionCount"`
}

// Status は provisioning-api の状態を取得する。
func (c *Client) Status(ctx context.Context) (Status, error) {
	return c.Call[Status](ctx, downstream.Request{Method: http.MethodGet, Path: []string{"status"}})
}

// hints は provisioning-api に接続できなかったときの対処の文。
var hints = downstream.Hints{
	HostnameMismatch: "provisioning-api のサーバー証明書に、接続先のホスト名（または IP アドレス）が入っていません。" +
		"本PoC側でサーバー証明書の SAN に接続先（同一ホストなら DNS:provisioning-api）を入れて作り直し" +
		"（本PoC B-02 §15.2）、provisioner にも渡し直してください。",
	UnknownServer: "provisioning-api のサーバー証明書が、設定した証明書（PROVISIONER_PROV_SERVER_CERT）と一致しません。" +
		"本PoCの deployments/certs/provisioning/server.pem を設定してください。",
	ServerCertInvalid: "provisioning-api のサーバー証明書が有効期間外です。本PoC側で作り直し（本PoC B-02 §15.2）、provisioner にも渡し直してください。",
	ClientRejected: "provisioning-api が provisioner のクライアント証明書を受け付けませんでした。" +
		"本PoC側の .env の PROVISIONING_API_ADMIN_CLIENTS に、このクライアント証明書のフィンガープリントが登録されているか" +
		"（登録後に provisioning-api を作り直したか）、証明書が有効期間内かを確認してください。",
	ConnectionReset: "provisioning-api が接続を切りました。provisioner のクライアント証明書が受け付けられなかった可能性があります。" +
		"本PoC側の .env の PROVISIONING_API_ADMIN_CLIENTS に、このクライアント証明書のフィンガープリントが登録されているか" +
		"（登録後に provisioning-api を作り直したか）、証明書が有効期間内かを確認してください" +
		"（provisioning-api のログに PROV_CLIENT_REJECTED が出ていれば、これが原因です）。",
	NameNotFound: "provisioning-api のホスト名（%s）を解決できません。本PoCを docker compose --profile provisioning up -d で" +
		"起動しているか、同一ホストの場合は provisioner が共有ネットワーク（eapaka-prov）に参加しているか確認してください。",
	DialFailed: "provisioning-api に接続できません。本PoCの provisioning-api が起動しているか（--profile provisioning）、" +
		"接続先の URL とポートが正しいか確認してください。",
	Timeout: "provisioning-api から時間内に応答がありません。接続先のアドレス、VPN、本PoC側の公開先（PROVISIONING_API_BIND）を確認してください。",
}

// Diagnose は、provisioning-api に接続できなかったときの原因の見当を返す。見当がつかなければ空文字列。
func Diagnose(err error) string { return downstream.Diagnose(err, hints) }
