package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ErrLocked は、同じ IMSI のロックを他の操作が持っていることを表す。
var ErrLocked = errors.New("locked by another operation")

func lockKey(imsi string) string { return "lock:imsi:" + imsi }

// releaseLockScript は、ロックの値（取得者のトークン）が一致するときだけ消す。
// 有効期限が切れて他の操作が取り直したロックを、誤って消さないため。
var releaseLockScript = valkey.NewLuaScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// AcquireIMSILock は IMSI のロックを取る（SET NX PX）。取れなければ ErrLocked を返す。
// 返したトークンを ReleaseIMSILock に渡して解放する。ttl を過ぎるとロックは自然に外れる。
func (s *Store) AcquireIMSILock(ctx context.Context, imsi string, ttl time.Duration) (token string, err error) {
	var b [16]byte
	rand.Read(b[:])
	token = hex.EncodeToString(b[:])
	err = s.c.Do(ctx, s.c.B().Set().Key(lockKey(imsi)).Value(token).Nx().Px(ttl).Build()).Error()
	switch {
	case valkey.IsValkeyNil(err):
		return "", ErrLocked
	case err != nil:
		return "", fmt.Errorf("acquire lock %s: %w", imsi, err)
	}
	return token, nil
}

// ReleaseIMSILock は IMSI のロックを解放する。他の操作が取り直したロックには触れない。
func (s *Store) ReleaseIMSILock(ctx context.Context, imsi, token string) error {
	if err := releaseLockScript.Exec(ctx, s.c, []string{lockKey(imsi)}, []string{token}).Error(); err != nil {
		return fmt.Errorf("release lock %s: %w", imsi, err)
	}
	return nil
}
