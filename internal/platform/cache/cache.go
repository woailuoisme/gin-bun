// Package cache 封装 Redis 客户端，承载令牌撤销名单与限流计数。
//
// 客户端是 rueidis：同一份实现对接 Redis 与 Valkey，默认开启自动流水线（并发命令共用一次
// 往返），适合本包"每个请求都会碰一两次、每次都要求低延迟"的用法。
//
// 本包刻意只提供应用真正需要的能力：键空间、读写、原子计数、cache-aside 回源。
// 数据结构、分布式锁、事务、发布订阅都不在这里——它们尚未出现调用方，写进来只会变成
// 一层需要维护与测试的 SDK。
package cache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/rueidis"
	"golang.org/x/sync/singleflight"

	"bgin/internal/config"
	"bgin/internal/platform/logger"
)

const (
	// defaultPort 是 REDIS_URL 未写端口时的默认值。
	defaultPort = "6379"
	// namespace 是本应用占用的键前缀。
	// 本机 Redis 是多个应用共享的，所有键都必须带它，以免与别的应用或人工调试的键相撞；
	// 键一律经 Key 构造，前缀与分隔符因此只有一处定义。
	namespace = "bgin"
	// opTimeout 是单次 Redis 操作的预算。
	//
	// 必须有：调用方通常直接把请求的 ctx 传进来，而它本身没有截止时间——Redis 卡住时，
	// 请求会跟着一起挂到更上层的超时才被砍掉。调用方的 ctx 更紧时沿用它的，绝不延长。
	opTimeout = 500 * time.Millisecond
)

// ErrNotFound 表示键不存在。它是 Get 的"未命中"而非故障，调用方据此回源。
var ErrNotFound = errors.New("cache: key not found")

// Client 是应用所需的 Redis 操作的窄封装。
type Client struct {
	rdb rueidis.Client
	// group 让 Remember 的并发回源只落一次。
	group singleflight.Group
}

// Key 按命名空间拼出一个键，例如 Key("jwt", "revoked", jti) → bgin:jwt:revoked:<jti>。
func Key(parts ...string) string {
	if len(parts) == 0 {
		return namespace
	}
	return namespace + ":" + strings.Join(parts, ":")
}

// Open 建立连接并做一次 PING 探测，Redis 不可用时直接失败而非带病启动。
func Open(ctx context.Context, cfg config.CacheConfig) (*Client, error) {
	option, err := clientOption(cfg.URL)
	if err != nil {
		return nil, err
	}

	rdb, err := rueidis.NewClient(option)
	if err != nil {
		return nil, fmt.Errorf("cache: connect Redis failed: %w", err)
	}

	client := &Client{rdb: rdb}
	if err := client.Ping(ctx); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("cache: ping Redis failed: %w", err)
	}
	return client, nil
}

// clientOption 把 REDIS_URL 翻译成 rueidis 的连接参数。
//
// 支持 redis:// 与 rediss://（后者启用 TLS）；用户信息里的口令映射到 Password，
// 路径里的库序号映射到 SelectDB。
func clientOption(rawURL string) (rueidis.ClientOption, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rueidis.ClientOption{}, fmt.Errorf("cache: parse REDIS_URL failed: %w", err)
	}

	switch parsed.Scheme {
	case "redis", "rediss":
	default:
		return rueidis.ClientOption{}, fmt.Errorf(
			"cache: unsupported REDIS_URL scheme %q, only redis:// or rediss:// allowed", parsed.Scheme)
	}

	// 省略端口时补默认端口：rueidis 的 InitAddress 需要 host:port 形式。
	host := parsed.Host
	if parsed.Port() == "" {
		host = net.JoinHostPort(parsed.Hostname(), defaultPort)
	}

	option := rueidis.ClientOption{
		InitAddress: []string{host},
		// 用户名为空时 rueidis 只发 AUTH <password>，对应 "redis://:pass@host" 这种写法；
		// ACL 场景下 URL 里会带用户名，届时按 AUTH <user> <password> 发送。
		Username: parsed.User.Username(),
	}
	if password, ok := parsed.User.Password(); ok {
		option.Password = password
	}

	if db := strings.TrimPrefix(parsed.Path, "/"); db != "" {
		index, err := strconv.Atoi(db)
		if err != nil {
			return rueidis.ClientOption{}, fmt.Errorf("cache: invalid database number %q in REDIS_URL: %w", db, err)
		}
		option.SelectDB = index
	}

	if parsed.Scheme == "rediss" {
		option.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return option, nil
}

// do 是所有命令的统一出口：先给这次操作套上预算，再把结果交回调用方。
// rueidis 的 Do 会把整条回复读进内存才返回，因此这里的 cancel 不会截断结果读取。
func (c *Client) do(ctx context.Context, cmd rueidis.Completed) rueidis.RedisResult {
	ctx, cancel := withOpTimeout(ctx)
	defer cancel()
	return c.rdb.Do(ctx, cmd)
}

// withOpTimeout 给操作套预算：调用方的 ctx 更紧就沿用它，绝不延长。
func withOpTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= opTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, opTimeout)
}

// Close 释放连接池。
func (c *Client) Close() error {
	c.rdb.Close()
	return nil
}

// Ping 探测连接是否可达，供启动期与就绪探针使用。
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, c.rdb.B().Ping().Build()).Error()
}

// Get 读取键的值；键不存在时返回 ErrNotFound。
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	value, err := c.do(ctx, c.rdb.B().Get().Key(key).Build()).ToString()
	switch {
	case err == nil:
		return value, nil
	case errors.Is(err, rueidis.Nil):
		return "", ErrNotFound
	default:
		return "", err
	}
}

// Set 写入一个带过期时间的键。ttl 必须为正数，避免误写永久键。
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("cache: ttl for key %s must be positive, got %s", key, ttl)
	}
	return c.do(ctx, c.rdb.B().Set().Key(key).Value(value).Ex(ttl).Build()).Error()
}

// SetNX 仅在键不存在时写入，返回是否写入成功。
//
// 它是幂等键与"抢锁"的原语：只有成功的那个调用方拥有该键，直到 ttl 到期。
// 注意这里的锁是单实例 Redis 上的锁，不含续期与所有权校验——需要这些语义时应当
// 由使用方明确实现，而不是由本包暗中提供。
func (c *Client) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("cache: ttl for key %s must be positive, got %s", key, ttl)
	}

	err := c.do(ctx, c.rdb.B().Set().Key(key).Value(value).Nx().Ex(ttl).Build()).Error()
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, rueidis.Nil):
		// SET NX 在键已存在时不写入，服务端返回空回复。
		return false, nil
	default:
		return false, err
	}
}

// Exists 判断键是否存在。
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	count, err := c.do(ctx, c.rdb.B().Exists().Key(key).Build()).AsInt64()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Increment 原子自增，并在键首次出现时设置过期时间，得到单调递增的固定窗口计数。
//
// 用 Lua 脚本而不是把 INCR 与 EXPIRE 两条命令流水线发出：流水线只共用一次往返、并不原子，
// 一旦第二条命令没落地，计数键会永久留在库里——那个客户端就被恒定限流了。脚本由 rueidis
// 缓存 sha 后走 EVALSHA，未命中时自动回退 EVAL。
func (c *Client) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, fmt.Errorf("cache: ttl for key %s must be positive, got %s", key, ttl)
	}

	// 取整到秒并向上取整：不足 1 秒会被 EXPIRE 解读为 0，而 EXPIRE key 0 是立即删除。
	seconds := int64((ttl + time.Second - 1) / time.Second)

	resp := incrementScript.Exec(ctx, c.rdb, []string{key}, []string{strconv.FormatInt(seconds, 10)})
	return resp.AsInt64()
}

// TTL 返回键的剩余寿命：键不存在时返回 0，键存在但没有过期时间时返回 -1s。
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	seconds, err := c.do(ctx, c.rdb.B().Ttl().Key(key).Build()).AsInt64()
	if err != nil {
		return 0, err
	}

	switch seconds {
	case -2: // 键不存在
		return 0, nil
	case -1: // 键存在但没有过期时间
		return -time.Second, nil
	default:
		return time.Duration(seconds) * time.Second, nil
	}
}

// Delete 删除若干键，返回实际删除数量。
func (c *Client) Delete(ctx context.Context, keys ...string) (int64, error) {
	// 一个键都不给时直接返回：DEL 不带参数会被服务端当成语法错误。
	if len(keys) == 0 {
		return 0, nil
	}
	return c.do(ctx, c.rdb.B().Del().Key(keys...).Build()).AsInt64()
}

// Remember 实现 cache-aside：命中直接返回；未命中则回源、写入缓存并返回。
//
// 同一键上的并发未命中只有一个调用方会执行 load，其余等它的结果（singleflight）。
// 两点边界要知道：
//
//   - 去重只发生在本进程内。多实例部署时每个实例仍会各自回源一次，跨实例防击穿需要锁。
//   - 缓存写失败不影响返回值——load 已经成功，写不进去只意味着下次再回源一次；
//     若把写失败当错误返回，一次缓存抖动就会让本来能成的请求失败。
func (c *Client) Remember(
	ctx context.Context,
	key string,
	ttl time.Duration,
	load func(context.Context) (string, error),
) (string, error) {
	cached, err := c.Get(ctx, key)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, ErrNotFound) {
		// 缓存本身不可用，不能当成未命中：那会把缓存故障放大成全量回源。
		return "", err
	}

	// 用 DoChan 而非 Do：Do 会把首个调用方（leader）的 ctx 共享给所有等待者，
	// leader 一旦被取消或超时，其余调用方会跟着收到 context.Canceled，
	// 而它们各自的截止时间对共享的 load 并不生效。DoChan 让每个调用方都能按自己的 ctx 退出。
	ch := c.group.DoChan(key, func() (any, error) {
		// 共享回源不能被任何单个调用方的取消信号带走：leader 的请求提前结束时，
		// 其余等待者仍需拿到结果、缓存也应照常写入。摘除取消信号但保留上下文值
		// （日志器、request_id 继续可用）；真正的超时约束由底层客户端自行负责。
		fillCtx := context.WithoutCancel(ctx)

		// 等锁期间缓存可能已被别的调用方填上，因此进来再查一次。
		cached, err := c.Get(fillCtx, key)
		if err == nil {
			return cached, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}

		loaded, err := load(fillCtx)
		if err != nil {
			return "", err
		}

		if err := c.Set(fillCtx, key, loaded, ttl); err != nil {
			logger.FromContext(fillCtx).Warn().Err(err).Str("key", key).Msg("failed to write origin data to cache")
		}
		return loaded, nil
	})

	var result any
	select {
	case res := <-ch:
		if res.Err != nil {
			return "", res.Err
		}
		result = res.Val
	case <-ctx.Done():
		return "", ctx.Err()
	}

	value, ok := result.(string)
	if !ok {
		return "", errors.New("cache: load result is not a string")
	}
	return value, nil
}

// incrementScript 见 Increment 的说明：自增与设过期必须在同一次原子执行里完成。
var incrementScript = rueidis.NewLuaScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)
