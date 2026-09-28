package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Locker 定义分布式互斥锁契约。
type Locker interface {
	// Acquire 尝试以非阻塞方式获取指定 key 的分布式锁。
	// 获取成功时返回释放函数和 acquired=true；已被占用时返回 acquired=false, err=nil。
	Acquire(ctx context.Context, key string, ttl time.Duration) (release func(context.Context) error, acquired bool, err error)
}

// TryLock 尝试获取分布式锁并在持有锁期间执行 fn。
// 若锁已被占用，则跳过执行并返回 acquired=false, err=nil。
func TryLock(ctx context.Context, locker Locker, key string, ttl time.Duration, fn func(ctx context.Context) error) (bool, error) {
	if locker == nil {
		return true, fn(ctx)
	}

	release, acquired, err := locker.Acquire(ctx, key, ttl)
	if err != nil {
		return false, err
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		_ = release(ctx)
	}()

	return true, fn(ctx)
}

// RedisLocker 基于 Redis SET NX PX 与 Lua 脚本提供安全释放机制的分布式锁实现。
type RedisLocker struct {
	client redis.UniversalClient
}

// NewRedisLocker 创建基于 Redis 的分布式锁实例。
func NewRedisLocker(client redis.UniversalClient) *RedisLocker {
	return &RedisLocker{client: client}
}

// luaReleaseLock 确保只有持有锁的客户端（token 匹配）才能删除释放锁。
const luaReleaseLock = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`

// Acquire 尝试通过 Redis SET key token NX PX ttl 获取锁。
func (l *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (func(context.Context) error, bool, error) {
	if l == nil || l.client == nil {
		return nil, false, errors.New("queue: redis client is nil")
	}

	token := randomToken()
	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	release := func(relCtx context.Context) error {
		return l.client.Eval(relCtx, luaReleaseLock, []string{key}, token).Err()
	}

	return release, true, nil
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}
