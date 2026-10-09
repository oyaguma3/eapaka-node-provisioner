package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const keyAudit = "audit"

// AuditEntry は provisioner の監査ログの 1 件（docs/openapi/provisioner-api.yaml の AuditLogEntry）。
type AuditEntry struct {
	ID          string    // エントリID。記録時は空
	Time        time.Time // 記録日時。エントリID から求める
	Operator    string    // 操作者（X-Operator-Id）
	MgmtClient  string    // 管理クライアントの識別名
	Action      string    // 操作（subscriber.create 等）
	Target      string    // 対象（IMSI、ID 等）
	TraceID     string    // トレースID
	OperationID string    // 操作の記録の ID（ない場合は空）
	Result      string    // 結果（completed 等）
	Details     string    // 変更内容の JSON（ない場合は空）
}

// AppendAudit は監査ログを追記する。件数が maxLen を超えた分は古いものから捨てる（おおよその上限）。
func (s *Store) AppendAudit(ctx context.Context, e AuditEntry, maxLen int64) error {
	cmd := s.c.B().Xadd().Key(keyAudit).Maxlen().Almost().Threshold(strconv.FormatInt(maxLen, 10)).Id("*").FieldValue().
		FieldValue("operator", e.Operator).
		FieldValue("mgmt_client", e.MgmtClient).
		FieldValue("action", e.Action).
		FieldValue("target", e.Target).
		FieldValue("trace_id", e.TraceID).
		FieldValue("operation_id", e.OperationID).
		FieldValue("result", e.Result).
		FieldValue("details", e.Details).Build()
	if err := s.c.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// ListAudit は監査ログを新しい順に返す。before が空でなければ、そのエントリID より古いものを返す。
// さらに古いエントリがあれば、次の before に渡す値を返す。
func (s *Store) ListAudit(ctx context.Context, before string, limit int) (entries []AuditEntry, nextBefore string, err error) {
	end := "+"
	if before != "" {
		end = "(" + before
	}
	// 続きがあるかを知るために 1 件多く読む。
	res, err := s.c.Do(ctx, s.c.B().Xrevrange().Key(keyAudit).End(end).Start("-").Count(int64(limit)+1).Build()).AsXRange()
	if err != nil {
		return nil, "", fmt.Errorf("list audit: %w", err)
	}
	if len(res) > limit {
		res = res[:limit]
		nextBefore = res[limit-1].ID
	}
	entries = make([]AuditEntry, len(res))
	for i, r := range res {
		f := r.FieldValues
		entries[i] = AuditEntry{
			ID: r.ID, Time: auditTime(r.ID),
			Operator: f["operator"], MgmtClient: f["mgmt_client"], Action: f["action"], Target: f["target"],
			TraceID: f["trace_id"], OperationID: f["operation_id"], Result: f["result"], Details: f["details"],
		}
	}
	return entries, nextBefore, nil
}

// auditTime はエントリID（<ミリ秒>-<連番>）から記録日時を求める。
func auditTime(id string) time.Time {
	ms, _, _ := strings.Cut(id, "-")
	v, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}
