// Package validator_test 验证请求绑定与校验的外部契约（api.md 2.4 与 5.2）。
package validator_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	govalidator "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/config"
	"bgin/internal/platform/errs"
	"bgin/internal/platform/i18n"
	"bgin/internal/platform/validator"
)

func init() { gin.SetMode(gin.TestMode) }

// signupPayload 覆盖本项目用到的全部自定义规则。
type signupPayload struct {
	Email       string  `json:"email" validate:"required,email"`
	Password    string  `json:"password" validate:"required,min=8"`
	Username    *string `json:"username" validate:"omitempty,min=3,usernameslug,notreserved"`
	PhoneNumber *string `json:"phoneNumber" validate:"omitempty,e164"`
}

// optionalBody 覆盖可省略请求体的行为。
type optionalBody struct {
	RefreshToken *string `json:"refreshToken" validate:"omitempty"`
}

// numericProbe 覆盖 min/max 在数值与字符串上的措辞分流。
type numericProbe struct {
	Count int    `json:"count" validate:"min=1"`
	Name  string `json:"name" validate:"min=1"`
}

// newTranslator 装载指定默认语言并构造 Translator。
func newTranslator(t *testing.T, defaultLanguage string) *i18n.Translator {
	t.Helper()

	translator, err := i18n.New(&config.Config{
		I18n: config.I18nConfig{DefaultLanguage: defaultLanguage},
	})
	require.NoError(t, err)
	return translator
}

// newContext 构造带请求体的 gin 上下文。
func newContext(body string) *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

// bindJSON 模拟 Gin 原生 ShouldBindJSON 并协同 translator 进行字段翻译。
func bindJSON(t *testing.T, c *gin.Context, target any, translator *i18n.Translator) error {
	t.Helper()
	err := c.ShouldBindJSON(target)
	if err == nil {
		return nil
	}
	if valErr, ok := errors.AsType[govalidator.ValidationErrors](err); ok {
		lang := c.GetHeader("Accept-Language")
		if translator != nil {
			lang = translator.Language(lang)
		}
		return errs.Validation(validator.Translate(translator, lang, valErr))
	}
	return err
}

// TestJSONRejectsEmptyBody 校验空请求体是 400 而非 422（api.md 4.2）。
func TestJSONRejectsEmptyBody(t *testing.T) {
	var payload signupPayload
	assertBadRequest(t, bindJSON(t, newContext(""), &payload, newTranslator(t, "en")))
}

// TestJSONRejectsMalformedJSON 校验非法 JSON 是 400。
func TestJSONRejectsMalformedJSON(t *testing.T) {
	var payload signupPayload
	assertBadRequest(t, bindJSON(t, newContext(`{"email":`), &payload, newTranslator(t, "en")))
}

// TestJSONRejectsWrongFieldType 校验字段类型不匹配是 400。
func TestJSONRejectsWrongFieldType(t *testing.T) {
	var payload signupPayload
	assertBadRequest(t, bindJSON(t, newContext(`{"email":42}`), &payload, newTranslator(t, "en")))
}

// TestJSONReportsCleanFieldPaths 校验错误键是纯字段路径，不含根结构体类型名。
func TestJSONReportsCleanFieldPaths(t *testing.T) {
	var payload signupPayload
	err := bindJSON(t, newContext(`{"email":"not-an-email","password":"short"}`), &payload, newTranslator(t, "en"))
	require.Error(t, err)

	status, resp := errs.Map(err)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, "Validation failed", resp.Message)
	assert.Equal(t, []string{"The email address must be a valid email address."}, resp.Errors["email"])
	assert.Equal(t, []string{"The password must be at least 8 characters."}, resp.Errors["password"])
}

// TestCustomRules 校验自定义规则的接受与拒绝分支。
func TestCustomRules(t *testing.T) {
	translator := newTranslator(t, "en")

	valid := `{"email":"a@b.com","password":"secret123","username":"alice_01","phoneNumber":"+8613800138000"}`
	var accepted signupPayload
	require.NoError(t, bindJSON(t, newContext(valid), &accepted, translator))

	rejected := []struct {
		name string
		body string
		key  string
	}{
		{"用户名含大写", `{"email":"a@b.com","password":"secret123","username":"Alice"}`, "username"},
		{"用户名过短", `{"email":"a@b.com","password":"secret123","username":"ab"}`, "username"},
		{"用户名含连字符", `{"email":"a@b.com","password":"secret123","username":"a-b"}`, "username"},
		{"用户名保留字 admin", `{"email":"a@b.com","password":"secret123","username":"admin"}`, "username"},
		{"用户名保留字 api", `{"email":"a@b.com","password":"secret123","username":"api"}`, "username"},
		{"手机号缺国际区号", `{"email":"a@b.com","password":"secret123","phoneNumber":"13800138000"}`, "phoneNumber"},
		{"缺少必填字段", `{}`, "email"},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			var payload signupPayload
			err := bindJSON(t, newContext(tt.body), &payload, translator)
			require.Error(t, err)

			status, resp := errs.Map(err)
			assert.Equal(t, http.StatusUnprocessableEntity, status)
			assert.Contains(t, resp.Errors, tt.key, "实际字段键: %v", resp.Errors)
		})
	}
}

// TestMinMaxWordingDependsOnKind 校验数值与字符串的 min 措辞分流。
func TestMinMaxWordingDependsOnKind(t *testing.T) {
	var probe numericProbe
	translator := newTranslator(t, "en")
	ctx := newContext(`{"count":0,"name":""}`)
	err := bindJSON(t, ctx, &probe, translator)
	require.Error(t, err)

	_, resp := errs.Map(err)
	assert.Equal(t, []string{"The count must be at least 1."}, resp.Errors["count"])
	assert.Equal(t, []string{"The name must be at least 1 characters."}, resp.Errors["name"])
}

// TestJSONOptionalToleratesEmptyBody 校验可省略请求体在空体时视为零值对象。
func TestJSONOptionalToleratesEmptyBody(t *testing.T) {
	var payload optionalBody
	require.NoError(t, validator.BindOptionalJSON(newContext(""), &payload))
	assert.Nil(t, payload.RefreshToken)
}

// TestJSONOptionalStillRejectsMalformedJSON 校验可省略请求体仍要求合法 JSON。
func TestJSONOptionalStillRejectsMalformedJSON(t *testing.T) {
	var payload optionalBody
	assertBadRequest(t, validator.BindOptionalJSON(newContext(`{"refresh`), &payload))
}

// TestLanguageNegotiatedFromHeader 校验校验文案随 Accept-Language 切换。
func TestLanguageNegotiatedFromHeader(t *testing.T) {
	translator := newTranslator(t, "en")
	ctx := newContext(`{"email":"bad","password":"short"}`)
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	assert.Equal(t, "zh-CN", translator.Language(ctx.GetHeader("Accept-Language")))

	var payload signupPayload
	err := bindJSON(t, ctx, &payload, translator)
	require.Error(t, err)

	_, resp := errs.Map(err)
	assert.Equal(t, []string{"邮箱地址必须是有效的邮箱地址。"}, resp.Errors["email"])
	assert.Equal(t, []string{"密码至少需要 8 个字符。"}, resp.Errors["password"])
}

// TestPasswordStrength 校验口令强度规则及其字段键与契约错误码。
func TestPasswordStrength(t *testing.T) {
	require.NoError(t, validator.PasswordStrength("password", "secret12345"))
	require.NoError(t, validator.PasswordStrength("password", "口令密码1234"))
	require.Error(t, validator.PasswordStrength("password", "onlyletters"))
	require.Error(t, validator.PasswordStrength("password", "12345678"))

	status, resp := errs.Map(validator.PasswordStrength("newPassword", "onlyletters"))
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, string(errs.CodeAuthPasswordTooWeak), resp.Code)
	assert.Contains(t, resp.Errors, "newPassword")
}

// assertBadRequest 断言错误被映射为 400 BAD_REQUEST。
func assertBadRequest(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)
	status, resp := errs.Map(err)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, string(errs.CodeBadRequest), resp.Code)
}

// TestUsernameSlugAndE164LengthGuards 验证长度截断防护，防止超长字符串进入正则。
func TestUsernameSlugAndE164LengthGuards(t *testing.T) {
	translator := newTranslator(t, "en")

	// 1. 用户名超长 (65 字符)
	longUsername := "a" + string(bytes.Repeat([]byte("b"), 65))
	ctx1 := newContext(`{"email":"ok@example.com","password":"password1","username":"` + longUsername + `"}`)
	var payload1 signupPayload
	err1 := bindJSON(t, ctx1, &payload1, translator)
	require.Error(t, err1)
	_, resp1 := errs.Map(err1)
	assert.Contains(t, resp1.Errors, "username")

	// 2. 手机号超长 (> 20 字符)
	longPhone := "+86138" + string(bytes.Repeat([]byte("0"), 20))
	ctx2 := newContext(`{"email":"ok@example.com","password":"password1","phoneNumber":"` + longPhone + `"}`)
	var payload2 signupPayload
	err2 := bindJSON(t, ctx2, &payload2, translator)
	require.Error(t, err2)
	_, resp2 := errs.Map(err2)
	assert.Contains(t, resp2.Errors, "phoneNumber")

	// 3. 手机号过短 (< 8 字符)
	shortPhone := "+86"
	ctx3 := newContext(`{"email":"ok@example.com","password":"password1","phoneNumber":"` + shortPhone + `"}`)
	var payload3 signupPayload
	err3 := bindJSON(t, ctx3, &payload3, translator)
	require.Error(t, err3)
	_, resp3 := errs.Map(err3)
	assert.Contains(t, resp3.Errors, "phoneNumber")
}

// TestCustomTagConcurrentSafety 验证 sync.Map 高并发注册与查询自定义规则的线程安全。
func TestCustomTagConcurrentSafety(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tag := fmt.Sprintf("custom_rule_%d", idx)
			_ = validator.RegisterRule(tag, func(fl govalidator.FieldLevel) bool {
				return true
			}, "custom_msg_key")
		}(i)
	}
	wg.Wait()
}
