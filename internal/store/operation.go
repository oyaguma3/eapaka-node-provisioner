package store

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// 操作の記録（設計概要 §9.3）。複数の下流にまたがる加入者の作成・変更・削除の、手順ごとの状態を残す。
// 秘密の値（Ki / OPc）は保存しない。

// ErrNotFound は対象が存在しないことを表す。
var ErrNotFound = errors.New("not found")

// 操作の状態。
const (
	OpRunning    = "running"
	OpCompleted  = "completed"
	OpRolledBack = "rolled_back"
	OpRetrying   = "retrying"
	OpFailed     = "failed"
	OpDismissed  = "dismissed"
)

// 手順の状態。
const (
	StepPending     = "pending"
	StepDone        = "done"
	StepFailed      = "failed"
	StepCompensated = "compensated"
	StepSkipped     = "skipped"
)

// keyActiveOps は未完了の操作の ID の索引（スコアは次に処理してよい時刻のミリ秒）。
const keyActiveOps = "ops:active"

func opKey(id string) string { return "op:" + id }

// StepError は手順の最後の失敗。
type StepError struct {
	// Status は下流の HTTP ステータス（接続できなかった場合は 0）。
	Status int       `json:"status,omitzero"`
	Cause  string    `json:"cause,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Time   time.Time `json:"time"`
}

// OperationStep は操作の手順。
type OperationStep struct {
	Name       string     `json:"name"`
	Downstream string     `json:"downstream"`
	State      string     `json:"state"`
	Error      *StepError `json:"error,omitempty"`
}

// Operation は操作の記録。
type Operation struct {
	ID       string
	Kind     string // subscriber.create / subscriber.update / subscriber.delete
	IMSI     string
	KeyStore string
	Status   string
	Steps    []OperationStep
	// Attempts は補償・やり直しを試みた回数（最初の要求を含まない）。
	Attempts int
	// NextAttemptAt は次に処理してよい時刻。実行中の操作では、要求が落ちたとみなす時刻（ロックの有効期限）。
	NextAttemptAt time.Time
	Operator      string
	MgmtClient    string
	TraceID       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// PrevPolicy は、変更の補償に使う変更前の認可ポリシー（JSON）。HadPolicy が false なら変更前はなかった。
	PrevPolicy string
	HadPolicy  bool
}

// Final は、操作がこれ以上処理されない状態かを返す。
func (op *Operation) Final() bool {
	switch op.Status {
	case OpCompleted, OpRolledBack, OpDismissed:
		return true
	}
	return false
}

// SaveOperation は操作の記録を書く（全体を置き換える）。未完了（running / retrying / failed）なら ops:active に入れ、
// 完了したら ops:active から外して retention の後に消えるようにする。
func (s *Store) SaveOperation(ctx context.Context, op Operation, retention time.Duration) error {
	steps, err := json.Marshal(op.Steps)
	if err != nil {
		return fmt.Errorf("save operation %s: %w", op.ID, err)
	}
	key := opKey(op.ID)
	cmds := valkey.Commands{
		s.c.B().Multi().Build(),
		s.c.B().Hset().Key(key).FieldValue().
			FieldValue("kind", op.Kind).
			FieldValue("imsi", op.IMSI).
			FieldValue("key_store", op.KeyStore).
			FieldValue("status", op.Status).
			FieldValue("steps", string(steps)).
			FieldValue("attempts", strconv.Itoa(op.Attempts)).
			FieldValue("next_attempt_at", formatMilli(op.NextAttemptAt)).
			FieldValue("operator", op.Operator).
			FieldValue("mgmt_client", op.MgmtClient).
			FieldValue("trace_id", op.TraceID).
			FieldValue("created_at", formatMilli(op.CreatedAt)).
			FieldValue("updated_at", formatMilli(op.UpdatedAt)).
			FieldValue("prev_policy", op.PrevPolicy).
			FieldValue("had_policy", boolField(op.HadPolicy)).Build(),
	}
	if op.Final() {
		cmds = append(cmds,
			s.c.B().Zrem().Key(keyActiveOps).Member(op.ID).Build(),
			s.c.B().Pexpire().Key(key).Milliseconds(retention.Milliseconds()).Build())
	} else {
		cmds = append(cmds,
			s.c.B().Zadd().Key(keyActiveOps).ScoreMember().ScoreMember(float64(op.NextAttemptAt.UnixMilli()), op.ID).Build(),
			s.c.B().Persist().Key(key).Build())
	}
	cmds = append(cmds, s.c.B().Exec().Build())
	for _, r := range s.c.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return fmt.Errorf("save operation %s: %w", op.ID, err)
		}
	}
	return nil
}

// GetOperation は操作の記録を取得する。なければ ErrNotFound。
func (s *Store) GetOperation(ctx context.Context, id string) (Operation, error) {
	m, err := s.c.Do(ctx, s.c.B().Hgetall().Key(opKey(id)).Build()).AsStrMap()
	if err != nil {
		return Operation{}, fmt.Errorf("get operation %s: %w", id, err)
	}
	if len(m) == 0 {
		return Operation{}, ErrNotFound
	}
	op := Operation{
		ID: id, Kind: m["kind"], IMSI: m["imsi"], KeyStore: m["key_store"], Status: m["status"],
		NextAttemptAt: parseMilli(m["next_attempt_at"]),
		Operator:      m["operator"], MgmtClient: m["mgmt_client"], TraceID: m["trace_id"],
		CreatedAt: parseMilli(m["created_at"]), UpdatedAt: parseMilli(m["updated_at"]),
		PrevPolicy: m["prev_policy"], HadPolicy: m["had_policy"] == "1",
	}
	op.Attempts, _ = strconv.Atoi(m["attempts"])
	if err := json.Unmarshal([]byte(m["steps"]), &op.Steps); err != nil {
		return Operation{}, fmt.Errorf("get operation %s: steps: %w", id, err)
	}
	return op, nil
}

func formatMilli(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return strconv.FormatInt(t.UnixMilli(), 10)
}

func parseMilli(v string) time.Time {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(n).UTC()
}

func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
