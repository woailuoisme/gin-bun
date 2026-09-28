// Package i18n 装载语言包，并负责语言协商与文案渲染。
package i18n

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"bgin/internal/config"
	"bgin/lang"
)

// 语言包内的分节名，与 lang/<语言>/<分节>.toml 一一对应。
const (
	sectionValidation = "validation"
	sectionFields     = "fields"
	sectionMail       = "mail"
	sectionInvoice    = "invoice"
)

// bundle 是一门语言的文案集合。
type bundle struct {
	validation map[string]string
	fields     map[string]string
	mail       map[string]string
	invoice    map[string]string
}

// Translator 按请求语言渲染文案；缺失的键一律回退到默认语言。
type Translator struct {
	fallback string
	bundles  map[string]*bundle
}

// New 装载内嵌的全部语言包。资源已编译进二进制，装载失败属于打包错误。
func New(cfg *config.Config) (*Translator, error) {
	entries, err := fs.ReadDir(lang.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("i18n: read language directory failed: %w", err)
	}

	bundles := make(map[string]*bundle, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loaded, err := loadBundle(entry.Name())
		if err != nil {
			return nil, err
		}
		bundles[entry.Name()] = loaded
	}
	if len(bundles) == 0 {
		return nil, errors.New("i18n: no language bundle found")
	}

	fallback := cfg.I18n.DefaultLanguage
	if _, ok := bundles[fallback]; !ok {
		return nil, fmt.Errorf("i18n: default language %q bundle not found", fallback)
	}
	return &Translator{fallback: fallback, bundles: bundles}, nil
}

var (
	defaultMu         sync.RWMutex
	defaultTranslator *Translator
)

// SetDefault 注册全局默认翻译器。
//
// 装配树里的调用方直接持有 Translator，本入口是给拿不到装配依赖的调用方准备的：
// PDF 文档模版的 Build 只收到 ctx 与 Builder，无法从签名上取到翻译器。
// 形态与 pdf.RegisterPDF / pdf.DefaultService 保持一致。
func SetDefault(translator *Translator) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTranslator = translator
}

// Default 返回全局默认翻译器；未注册时为 nil，调用方须自行判断。
func Default() *Translator {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultTranslator
}

// loadBundle 读取单门语言的全部文案文件。
func loadBundle(name string) (*bundle, error) {
	loaded := &bundle{
		validation: make(map[string]string),
		fields:     make(map[string]string),
		mail:       make(map[string]string),
		invoice:    make(map[string]string),
	}

	sections := map[string]map[string]string{
		sectionValidation: loaded.validation,
		sectionFields:     loaded.fields,
		sectionMail:       loaded.mail,
		sectionInvoice:    loaded.invoice,
	}
	for section, target := range sections {
		if err := decode(lang.FS, name+"/"+section+".toml", &target); err != nil {
			return nil, err
		}
	}
	return loaded, nil
}

// decode 读取并解析一个 TOML 资源文件。
func decode(fsys fs.FS, name string, dst any) error {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("i18n: read %s failed: %w", name, err)
	}
	if err := toml.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("i18n: decode %s failed: %w", name, err)
	}
	return nil
}

// Language 从 Accept-Language 协商出可用语言，无法匹配时回退默认语言。
// 按优先级逐个候选取值尝试，谁先命中就用谁。
func (t *Translator) Language(acceptLanguage string) string {
	for part := range strings.SplitSeq(acceptLanguage, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if tag == "" {
			continue
		}
		if matched := t.match(tag); matched != "" {
			return matched
		}
	}
	return t.fallback
}

// match 依次尝试精确匹配、逐级去掉子标签，最后按主语言前缀匹配已有语言包。
// 因此 en-GB 命中 en，zh-Hans-CN 命中 zh-CN。
func (t *Translator) match(tag string) string {
	for candidate := tag; candidate != ""; {
		if _, ok := t.bundles[candidate]; ok {
			return candidate
		}

		prefix, _, found := strings.CutLast(candidate, "-")
		if !found {
			break
		}
		candidate = prefix
	}

	primary, _, _ := strings.Cut(tag, "-")
	for _, name := range t.sortedNames() {
		if name == primary || strings.HasPrefix(name, primary+"-") {
			return name
		}
	}
	return ""
}

// sortedNames 返回排序后的语言包名，保证前缀匹配的结果稳定可预期。
func (t *Translator) sortedNames() []string {
	return slices.Sorted(maps.Keys(t.bundles))
}

// ValidationMessage 渲染一条校验失败文案；键或语言缺失时回退默认语言。
func (t *Translator) ValidationMessage(language, key, field, param string) string {
	tpl := t.template(language, sectionValidation, key)
	if tpl == "" {
		return ""
	}

	label := cmp.Or(t.FieldLabel(language, field), humanize(field))
	return render(tpl, map[string]string{"Field": label, "Param": param})
}

// Mail 渲染一封邮件的主题与正文；共用一个 <key>_subject / <key>_body 的键对。
func (t *Translator) Mail(language, key string, vars map[string]string) (subject, body string) {
	subject = render(t.template(language, sectionMail, key+"_subject"), vars)
	body = render(t.template(language, sectionMail, key+"_body"), vars)
	return subject, body
}

// Invoice 渲染一条发票/结算凭据的展示文案；键或语言缺失时回退默认语言。
//
// 它是 invoice 分节的取值入口，与 Mail 同构：PDF 模板只声明键，
// 展示文案一律留在 lang/<语言>/invoice.toml，模板里不留硬编码文案。
func (t *Translator) Invoice(language, key string, vars map[string]string) string {
	return render(t.template(language, sectionInvoice, key), vars)
}

// FieldLabel 返回字段路径对应的可读标签，缺键时返回空串交由调用方兜底。
func (t *Translator) FieldLabel(language, field string) string {
	if b, ok := t.bundles[language]; ok {
		if label, ok := b.fields[field]; ok {
			return label
		}
	}
	if b, ok := t.bundles[t.fallback]; ok {
		return b.fields[field]
	}
	return ""
}

// template 按语言与分节取模板，缺键时用默认语言兜底。
func (t *Translator) template(language, section, key string) string {
	if tpl := lookup(t.bundles[language], section, key); tpl != "" {
		return tpl
	}
	return lookup(t.bundles[t.fallback], section, key)
}

// lookup 从语言包中取出指定分节的模板。
func lookup(b *bundle, section, key string) string {
	if b == nil {
		return ""
	}

	switch section {
	case sectionValidation:
		return b.validation[key]
	case sectionFields:
		return b.fields[key]
	case sectionMail:
		return b.mail[key]
	case sectionInvoice:
		return b.invoice[key]
	default:
		return ""
	}
}

// render 替换模板中的 {{.Key}} 占位符。
func render(tpl string, vars map[string]string) string {
	if tpl == "" || len(vars) == 0 {
		return tpl
	}

	pairs := make([]string, 0, len(vars)*2)
	for key, value := range vars {
		pairs = append(pairs, "{{."+key+"}}", value)
	}
	return strings.NewReplacer(pairs...).Replace(tpl)
}

// humanize 把字段路径的最后一段（camelCase）转成空格分隔的小写短语。
func humanize(field string) string {
	_, segment, found := strings.CutLast(field, ".")
	if !found {
		segment = field
	}
	if segment == "" {
		return field
	}

	var b strings.Builder
	b.Grow(len(segment) + 4)
	for i, r := range segment {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
