// Package middleware 提供 HTTP 横切中间件。
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"bgin/internal/config"
	"bgin/internal/platform/authz"
	"bgin/internal/platform/cache"
	"bgin/internal/platform/i18n"
	"bgin/internal/platform/token"
)

// Deps 是中间件所需的全部依赖，由 main 装配后注入。
type Deps struct {
	Tokens         *token.Issuer
	Cache          *cache.Client
	Authz          *authz.Enforcer
	Logger         *zerolog.Logger
	Limiter        config.LimiterConfig
	CORS           config.CORSConfig
	Production     bool
	Translator     *i18n.Translator
	RequestTimeout time.Duration
}

// Middleware 持有横切逻辑所需的依赖。
type Middleware struct {
	tokens         *token.Issuer
	cache          *cache.Client
	authz          *authz.Enforcer
	logger         *zerolog.Logger
	limiter        config.LimiterConfig
	cors           config.CORSConfig
	production     bool
	translator     *i18n.Translator
	requestTimeout time.Duration
}

// New 构造 Middleware。
func New(deps Deps) *Middleware {
	return &Middleware{
		tokens:         deps.Tokens,
		cache:          deps.Cache,
		authz:          deps.Authz,
		logger:         deps.Logger,
		limiter:        deps.Limiter,
		cors:           deps.CORS,
		production:     deps.Production,
		translator:     deps.Translator,
		requestTimeout: deps.RequestTimeout,
	}
}

// abortWithError 向上下文附加错误并立即中止后续处理。
func abortWithError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}
