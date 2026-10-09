package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// IdempotencyState は、Idempotency-Key の要求を始めたときの状態。
type IdempotencyState int

const (
	// IdempotencyNew は初めての要求（処理してよい）。
	IdempotencyNew IdempotencyState = iota
	// IdempotencyInProgress は、同じキーの最初の要求がまだ処理中。
	IdempotencyInProgress
	// IdempotencyMismatch は、同じキーで内容の違う要求が以前に届いている。
	IdempotencyMismatch
	// IdempotencyReplay は、同じ要求の応答を覚えている（それを返す）。
	IdempotencyReplay
)

// StoredResponse は、Idempotency-Key の要求に対して覚えておく応答。書き込みの応答なので秘密の値は含まない。
type StoredResponse struct {
	Status      int
	ContentType string
	Location    string
	Body        []byte
}

// idempotencyKey は Valkey のキー。管理クライアントごとに分け、キーの値はハッシュにして長さと文字を揃える。
func idempotencyKey(mgmtClient, key string) string {
	sum := sha256.Sum256([]byte(key))
	return "idem:" + mgmtClient + ":" + hex.EncodeToString(sum[:])
}

// beginIdempotencyScript は、初めてのキーなら処理中として記録する。既にあれば、内容と状態を比べて返す。
var beginIdempotencyScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('HSET', KEYS[1], 'req', ARGV[1], 'state', 'pending')
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return {'new'}
end
local v = redis.call('HMGET', KEYS[1], 'req', 'state', 'status', 'content_type', 'location', 'body')
if v[1] ~= ARGV[1] then
  return {'mismatch'}
end
if v[2] ~= 'done' then
  return {'pending'}
end
return {'done', v[3], v[4] or '', v[5] or '', v[6] or ''}
`)

// completeIdempotencyScript は、処理中の記録（内容が一致するもの）に応答を書き、有効期限を延ばす。
var completeIdempotencyScript = valkey.NewLuaScript(`
if redis.call('HGET', KEYS[1], 'req') ~= ARGV[1] then
  return 0
end
redis.call('HSET', KEYS[1], 'state', 'done', 'status', ARGV[2], 'content_type', ARGV[3], 'location', ARGV[4], 'body', ARGV[5])
redis.call('PEXPIRE', KEYS[1], ARGV[6])
return 1
`)

// abandonIdempotencyScript は、処理中の記録（内容が一致するもの）を消す。
var abandonIdempotencyScript = valkey.NewLuaScript(`
if redis.call('HGET', KEYS[1], 'req') == ARGV[1] and redis.call('HGET', KEYS[1], 'state') == 'pending' then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// BeginIdempotent は Idempotency-Key の要求を始める。初めてなら処理中として pendingTTL のあいだ記録する
// （プロセスが落ちても、その時間が過ぎればやり直せる）。reqHash は要求の内容（メソッド・パス・本文）のハッシュ。
func (s *Store) BeginIdempotent(ctx context.Context, mgmtClient, key, reqHash string, pendingTTL time.Duration) (IdempotencyState, StoredResponse, error) {
	res, err := beginIdempotencyScript.Exec(ctx, s.c, []string{idempotencyKey(mgmtClient, key)},
		[]string{reqHash, strconv.FormatInt(pendingTTL.Milliseconds(), 10)}).ToArray()
	if err != nil {
		return 0, StoredResponse{}, fmt.Errorf("begin idempotent request: %w", err)
	}
	state, err := res[0].ToString()
	if err != nil {
		return 0, StoredResponse{}, fmt.Errorf("begin idempotent request: %w", err)
	}
	switch state {
	case "new":
		return IdempotencyNew, StoredResponse{}, nil
	case "mismatch":
		return IdempotencyMismatch, StoredResponse{}, nil
	case "pending":
		return IdempotencyInProgress, StoredResponse{}, nil
	}
	var v [4]string
	for i := range v {
		if v[i], err = res[i+1].ToString(); err != nil {
			return 0, StoredResponse{}, fmt.Errorf("begin idempotent request: %w", err)
		}
	}
	status, err := strconv.Atoi(v[0])
	if err != nil {
		return 0, StoredResponse{}, fmt.Errorf("begin idempotent request: bad stored status %q", v[0])
	}
	return IdempotencyReplay, StoredResponse{Status: status, ContentType: v[1], Location: v[2], Body: []byte(v[3])}, nil
}

// CompleteIdempotent は応答を覚え、ttl のあいだ同じ要求にそれを返せるようにする。
func (s *Store) CompleteIdempotent(ctx context.Context, mgmtClient, key, reqHash string, resp StoredResponse, ttl time.Duration) error {
	err := completeIdempotencyScript.Exec(ctx, s.c, []string{idempotencyKey(mgmtClient, key)}, []string{
		reqHash, strconv.Itoa(resp.Status), resp.ContentType, resp.Location, string(resp.Body), strconv.FormatInt(ttl.Milliseconds(), 10),
	}).Error()
	if err != nil {
		return fmt.Errorf("complete idempotent request: %w", err)
	}
	return nil
}

// AbandonIdempotent は処理中の記録を消し、同じキーでやり直せるようにする（応答を覚えない場合に使う）。
func (s *Store) AbandonIdempotent(ctx context.Context, mgmtClient, key, reqHash string) error {
	if err := abandonIdempotencyScript.Exec(ctx, s.c, []string{idempotencyKey(mgmtClient, key)}, []string{reqHash}).Error(); err != nil {
		return fmt.Errorf("abandon idempotent request: %w", err)
	}
	return nil
}
