// Package middleware 提供 HTTP 横切中间件。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"bgin/internal/config"
	"bgin/internal/platform/authentication"
	"bgin/internal/platform/authorization"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/i18n"
)

// Deps 是中间件所需的全部依赖，由 main 装配后注入。
type Deps struct {
	Tokens         *authentication.TokenIssuer
	AuthManager    *authentication.Manager
	Cache          *cache.Client
	Authz          *authorization.Enforcer
	Logger         *zap.Logger
	Limiter        config.LimiterConfig
	CORS           config.CORSConfig
	Production     bool
	Translator     *i18n.Translator
	RequestTimeout time.Duration
	// MaxBody 非 multipart 请求的请求体上限（字节，0 表示用默认值）。
	MaxBody int64
	// UploadMaxBody multipart/form-data 上传请求的请求体上限（字节，0 表示用默认值）。
	UploadMaxBody int64
}

// Middleware 持有横切逻辑所需的依赖。
type Middleware struct {
	tokens         *authentication.TokenIssuer
	authManager    *authentication.Manager
	cache          *cache.Client
	authz          *authorization.Enforcer
	logger         *zap.Logger
	limiter        config.LimiterConfig
	cors           config.CORSConfig
	production     bool
	translator     *i18n.Translator
	requestTimeout time.Duration
	maxBody        int64
	uploadMaxBody  int64
}

// New 构造 Middleware。
func New(deps Deps) *Middleware {
	return &Middleware{
		tokens:         deps.Tokens,
		authManager:    deps.AuthManager,
		cache:          deps.Cache,
		authz:          deps.Authz,
		logger:         deps.Logger,
		limiter:        deps.Limiter,
		cors:           deps.CORS,
		production:     deps.Production,
		translator:     deps.Translator,
		requestTimeout: deps.RequestTimeout,
		maxBody:        deps.MaxBody,
		uploadMaxBody:  deps.UploadMaxBody,
	}
}

// abortWithError 向上下文附加错误并立即中止后续处理。
func abortWithError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}
