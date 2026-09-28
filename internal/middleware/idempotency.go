package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"bgin/internal/platform/cache"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/identity"
	"bgin/internal/platform/logger"
)

// HeaderIdempotencyKey IETF / Stripe 标准幂等键标头名。
const HeaderIdempotencyKey = "Idempotency-Key"

// AltHeaderIdempotencyKey 传统备用幂等键标头名。
const AltHeaderIdempotencyKey = "X-Idempotency-Key"

// HeaderIdempotentReplayed 标记该响应是否由幂等缓存直接回放。
const HeaderIdempotentReplayed = "Idempotent-Replayed"

// DefaultIdempotencyLockTTL 默认并发锁定有效期（30秒）。
const DefaultIdempotencyLockTTL = 30 * time.Second

// DefaultIdempotencyCacheTTL 默认成功响应缓存保留有效期（24小时）。
const DefaultIdempotencyCacheTTL = 24 * time.Hour

// IdempotencyConfig 定义请求幂等中间件配置选项。
type IdempotencyConfig struct {
	// Header 主幂等键标头，默认 "Idempotency-Key"。
	Header string
	// AltHeader 备用幂等键标头，默认 "X-Idempotency-Key"。
	AltHeader string
	// Enforce 是否强制要求必须携带幂等键，默认 false（宽松探测模式）。
	Enforce bool
	// LockTTL 处理中并发互斥锁的超时时长，默认 30 秒。
	LockTTL time.Duration
	// CacheTTL 处理成功响应的缓存保留时长，默认 24 小时。
	CacheTTL time.Duration
	// Methods 参与幂等控制的 HTTP 方法列表，默认 POST, PATCH。
	Methods []string
}

// IdempotencyOption 定义函数式配置项。
type IdempotencyOption func(*IdempotencyConfig)

// WithIdempotencyEnforced 设置是否开启强制校验模式。
func WithIdempotencyEnforced(enforced bool) IdempotencyOption {
	return func(c *IdempotencyConfig) {
		c.Enforce = enforced
	}
}

// WithIdempotencyLifetime 设置成功响应的缓存保留时长。
func WithIdempotencyLifetime(d time.Duration) IdempotencyOption {
	return func(c *IdempotencyConfig) {
		if d > 0 {
			c.CacheTTL = d
		}
	}
}

// WithIdempotencyLockTimeout 设置并发抢锁的过期时长。
func WithIdempotencyLockTimeout(d time.Duration) IdempotencyOption {
	return func(c *IdempotencyConfig) {
		if d > 0 {
			c.LockTTL = d
		}
	}
}

// WithIdempotencyMethods 指定受幂等性控制的 HTTP 方法集合。
func WithIdempotencyMethods(methods ...string) IdempotencyOption {
	return func(c *IdempotencyConfig) {
		if len(methods) > 0 {
			c.Methods = methods
		}
	}
}

type idempotencyStatus string

const (
	statusProcessing idempotencyStatus = "processing"
	statusCompleted  idempotencyStatus = "completed"
)

type idempotencyRecord struct {
	Status      idempotencyStatus `json:"status"`
	Fingerprint string            `json:"fingerprint"`
	StatusCode  int               `json:"status_code,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	CreatedAt   int64             `json:"created_at"`
}

type responseBufferWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *responseBufferWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseBufferWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseBufferWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func (w *responseBufferWriter) Status() int {
	if w.statusCode != 0 {
		return w.statusCode
	}
	return w.ResponseWriter.Status()
}

// IdempotencyStore 定义幂等中间件所需的存储原语接口。
// platform/cache.Client 结构体天然完整实现该接口。
type IdempotencyStore interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) (int64, error)
}

// Idempotency 基于 Middleware 依赖构造符合 IETF / Stripe 行业规范的 Gin 请求幂等中间件。
func (m *Middleware) Idempotency(opts ...IdempotencyOption) gin.HandlerFunc {
	return NewIdempotency(m.cache, opts...)
}

// NewIdempotency 构造独立的 Gin 幂等中间件实例，方便测试与自定义注入。
func NewIdempotency(store IdempotencyStore, opts ...IdempotencyOption) gin.HandlerFunc {
	cfg := defaultIdempotencyConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	methodsSet := make(map[string]struct{}, len(cfg.Methods))
	for _, method := range cfg.Methods {
		methodsSet[strings.ToUpper(method)] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := methodsSet[c.Request.Method]; !ok {
			c.Next()
			return
		}

		rawKey, valid := extractIdempotencyKey(c, &cfg)
		if !valid {
			return
		}
		if rawKey == "" || store == nil {
			c.Next()
			return
		}

		bodyBytes, err := readAndRewindRequestBody(c)
		if err != nil {
			_ = c.Error(errs.BadRequest("Failed to read request body"))
			c.Abort()
			return
		}

		fingerprint := computeRequestFingerprint(c.Request.Method, c.Request.URL.RequestURI(), bodyBytes)
		scope := resolveIdempotencyScope(c)
		cacheKey := cache.Key("idempotency", scope, rawKey)

		if !acquireOrHandleExisting(c, store, cacheKey, fingerprint, cfg.LockTTL) {
			return
		}

		bufferWriter := &responseBufferWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(make([]byte, 0, 1024)),
		}
		c.Writer = bufferWriter

		defer handlePanicRelease(c.Request.Context(), store, cacheKey)

		c.Next()

		finishIdempotentRequest(c.Request.Context(), store, cacheKey, fingerprint, bufferWriter, cfg.CacheTTL)
	}
}

func defaultIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		Header:    HeaderIdempotencyKey,
		AltHeader: AltHeaderIdempotencyKey,
		Enforce:   false,
		LockTTL:   DefaultIdempotencyLockTTL,
		CacheTTL:  DefaultIdempotencyCacheTTL,
		Methods:   []string{http.MethodPost, http.MethodPatch},
	}
}

// extractIdempotencyKey 从请求中提取并验证幂等键。
func extractIdempotencyKey(c *gin.Context, cfg *IdempotencyConfig) (string, bool) {
	rawKey := strings.TrimSpace(c.GetHeader(cfg.Header))
	if rawKey == "" && cfg.AltHeader != "" {
		rawKey = strings.TrimSpace(c.GetHeader(cfg.AltHeader))
	}

	if rawKey == "" {
		if cfg.Enforce {
			_ = c.Error(errs.IdempotencyRequired(""))
			c.Abort()
			return "", false
		}
		return "", true
	}

	if len(rawKey) > 128 {
		_ = c.Error(errs.BadRequest("Idempotency key length exceeds 128 characters"))
		c.Abort()
		return "", false
	}

	return rawKey, true
}

// readAndRewindRequestBody 读取并重置 Request Body，便于指纹计算与后续绑定。
func readAndRewindRequestBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return bodyBytes, nil
}

// acquireOrHandleExisting 尝试抢占锁；若键已存在则处理并发冲突、指纹比对或回放，返回 true 表示应继续执行。
func acquireOrHandleExisting(c *gin.Context, store IdempotencyStore, cacheKey, fingerprint string, lockTTL time.Duration) bool {
	ctx := c.Request.Context()
	lockRec := idempotencyRecord{
		Status:      statusProcessing,
		Fingerprint: fingerprint,
		CreatedAt:   time.Now().Unix(),
	}
	lockPayload, _ := json.Marshal(lockRec)

	acquired, err := store.SetNX(ctx, cacheKey, string(lockPayload), lockTTL)
	if err != nil {
		logger.FromContext(ctx).Error().Err(err).Str("key", cacheKey).Msg("idempotency cache SetNX failed")
		c.Next()
		return false
	}
	if acquired {
		return true
	}

	existingVal, err := store.Get(ctx, cacheKey)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			c.Next()
			return false
		}
		logger.FromContext(ctx).Error().Err(err).Str("key", cacheKey).Msg("failed to read idempotency cache")
		c.Next()
		return false
	}

	return !handleExistingRecord(c, existingVal, fingerprint)
}

// handleExistingRecord 处理既有记录：判断是否在处理中、指纹是否吻合或执行响应回放。
func handleExistingRecord(c *gin.Context, existingVal, currentFingerprint string) bool {
	var existingRec idempotencyRecord
	if err := json.Unmarshal([]byte(existingVal), &existingRec); err != nil {
		logger.FromContext(c.Request.Context()).Error().Err(err).Msg("failed to parse idempotency record")
		c.Next()
		return true
	}

	if existingRec.Status == statusProcessing {
		_ = c.Error(errs.IdempotencyConflict(""))
		c.Abort()
		return true
	}

	if existingRec.Fingerprint != currentFingerprint {
		_ = c.Error(errs.IdempotencyMismatch(""))
		c.Abort()
		return true
	}

	replayIdempotentResponse(c, existingRec)
	return true
}

// replayIdempotentResponse 直接回放已缓存的响应结果。
func replayIdempotentResponse(c *gin.Context, rec idempotencyRecord) {
	for k, v := range rec.Headers {
		c.Header(k, v)
	}
	c.Header(HeaderIdempotentReplayed, "true")

	contentType := rec.Headers["Content-Type"]
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(rec.StatusCode, contentType, []byte(rec.Body))
	c.Abort()
}

// handlePanicRelease 发生 panic 崩溃时立即删除锁，确保客户端能够安全重试。
func handlePanicRelease(ctx context.Context, store IdempotencyStore, cacheKey string) {
	if r := recover(); r != nil {
		_, _ = store.Delete(ctx, cacheKey)
		panic(r)
	}
}

// finishIdempotentRequest 评估状态码并沉淀成功缓存。
//
// 只沉淀 2xx：失败响应（4xx/5xx）一律释放锁，让客户端携带同一幂等键重试能够真正重放，
// 而不是在 24 小时缓存期内反复拿到同一个失败快照（例如令牌过期产生的 401、限流产生的 429）。
// 状态码为 0 表示响应体尚未写出（仅 c.Error + c.Abort，由后置 ErrorHandler 渲染），
// 此刻无从判断成败，同样释放锁并跳过缓存，绝不能记成 200 后固化。
func finishIdempotentRequest(ctx context.Context, store IdempotencyStore, cacheKey, fingerprint string, w *responseBufferWriter, ttl time.Duration) {
	finalStatusCode := w.Status()
	if finalStatusCode < http.StatusOK || finalStatusCode >= http.StatusMultipleChoices {
		_, _ = store.Delete(ctx, cacheKey)
		return
	}

	headers := make(map[string]string)
	if ct := w.Header().Get("Content-Type"); ct != "" {
		headers["Content-Type"] = ct
	}

	completedRec := idempotencyRecord{
		Status:      statusCompleted,
		Fingerprint: fingerprint,
		StatusCode:  finalStatusCode,
		Headers:     headers,
		Body:        w.body.String(),
		CreatedAt:   time.Now().Unix(),
	}
	completedPayload, err := json.Marshal(completedRec)
	if err == nil {
		if err := store.Set(ctx, cacheKey, string(completedPayload), ttl); err != nil {
			logger.FromContext(ctx).Warn().Err(err).Str("key", cacheKey).Msg("failed to persist completed idempotency record")
		}
	}
}

// computeRequestFingerprint 对 Method、URI 与 Body 计算 SHA-256 哈希指纹。
func computeRequestFingerprint(method, uri string, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(uri))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// resolveIdempotencyScope 区分用户与客户端隔离作用域。
//
// 幂等中间件注册在全局链上，早于路由级鉴权执行，此刻 context 中尚无身份，
// 因此不能只依赖 identity（那会让所有已认证请求退化成按 IP 共享缓存，
// 同一 NAT 下的两个用户可能互相回放到对方的响应体）。
//
// 这里退一步以访问令牌的 sub 声明（用户标识）作为隔离维度，理由有两点：
//   - 它只用于缓存命名空间分片，不承担任何鉴权职责，真正的鉴权仍由 RequireAuth 完成；
//   - 用 sub 而不是整串凭据，访问令牌轮换后仍落在同一命名空间，
//     同一幂等键的重试不会被当成新请求而重复执行（这正是幂等键的核心承诺）。
//
// 伪造 sub 需要同时掌握受害者的幂等键与请求指纹才可能命中其缓存，风险与按 IP 分片同级。
func resolveIdempotencyScope(c *gin.Context) string {
	if id, ok := identity.FromContext(c.Request.Context()); ok && id.UserID != "" {
		return "user:" + id.UserID
	}
	if subject := credentialSubject(c.GetHeader("Authorization")); subject != "" {
		// 先哈希再入键：sub 属不可信输入，哈希可同时约束键长并把原始标识挡在缓存键之外。
		sum := sha256.Sum256([]byte(subject))
		return "subject:" + hex.EncodeToString(sum[:16])
	}
	return "ip:" + c.ClientIP()
}

// credentialSubject 从 Authorization 头的 JWT 载荷中取出 sub 声明。
//
// 刻意不校验签名：本函数的产出只用于缓存分区，校签属鉴权中间件的职责。
// 解析失败（非 JWT、段数异常、载荷非 JSON、无 sub）时返回空串，调用方退回按 IP 分片。
func credentialSubject(header string) string {
	credential := bearerCredential(header)
	if credential == "" {
		return ""
	}

	parts := strings.Split(credential, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Subject)
}

// bearerCredential 提取 Authorization 头中的凭据本体，缺失或非 Bearer 方案时返回空串。
func bearerCredential(header string) string {
	scheme, value, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}
