package core

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"bgin/internal/console/stubs"
)

// GeneratorOptions 封装了代码生成器的入参配置。
type GeneratorOptions struct {
	// Name 是模块名或实体名（支持 CamelCase, snake_case, kebab-case 等格式）
	Name string
	// IsAdmin 标识是否生成管理端模块（默认为 true，路径为 internal/features/admin/<name>/）
	IsAdmin bool
	// TagName 自定义 Swagger 标签名（若留空则自动推导）
	TagName string
	// ChineseName 中文业务名称描述（若留空则默认为 PascalName）
	ChineseName string
	// Force 是否强制覆盖已存在的文件
	Force bool
	// DryRun 是否仅做生成预览而不实际写入文件系统
	DryRun bool
	// RootDir 项目根目录路径（默认为当前工作目录）
	RootDir string
}

// TemplateData 注入到模板渲染上下文的元数据对象。
type TemplateData struct {
	PackageName  string
	PascalName   string
	CamelName    string
	SnakeName    string
	KebabName    string
	PluralSnake  string
	PluralPascal string
	PluralKebab  string
	TableName    string
	TableAlias   string
	TagName      string
	ChineseName  string
	RoutePath    string
	IsAdmin      bool
}

// FileAction 标识单个生成文件的处理结果动作。
type FileAction string

const (
	ActionCreated     FileAction = "CREATED"
	ActionOverwritten FileAction = "OVERWRITTEN"
	ActionSkipped     FileAction = "SKIPPED"
	ActionDryRun      FileAction = "DRY-RUN"
	ActionDeleted     FileAction = "DELETED"
)

// GeneratedResult 记录单个文件的生成详情。
type GeneratedResult struct {
	Path     string
	Template string
	Action   FileAction
	Message  string
}

// BuildTemplateData 根据输入选项计算全部命名衍生变量。
func BuildTemplateData(opts GeneratorOptions) TemplateData {
	raw := strings.TrimSpace(opts.Name)
	snake := ToSnakeCase(raw)
	singularSnake := Singularize(snake)
	pluralSnake := Pluralize(snake)

	pascal := ToPascalCase(singularSnake)
	camel := ToCamelCase(singularSnake)
	kebab := ToKebabCase(singularSnake)
	pluralPascal := ToPascalCase(pluralSnake)
	pluralKebab := ToKebabCase(pluralSnake)

	pkgName := strings.ReplaceAll(singularSnake, "_", "")
	if opts.IsAdmin {
		pkgName = "admin" + pkgName
	}

	chinese := opts.ChineseName
	if chinese == "" {
		chinese = pascal
	}

	tag := opts.TagName
	if tag == "" {
		if opts.IsAdmin {
			tag = chinese + "管理"
		} else {
			tag = chinese
		}
	}

	routePath := "/" + pluralKebab
	if opts.IsAdmin {
		routePath = "/admin/" + pluralKebab
	}

	tableAlias := "t"
	if len(singularSnake) > 0 {
		tableAlias = singularSnake[:1]
	}

	return TemplateData{
		PackageName:  pkgName,
		PascalName:   pascal,
		CamelName:    camel,
		SnakeName:    singularSnake,
		KebabName:    kebab,
		PluralSnake:  pluralSnake,
		PluralPascal: pluralPascal,
		PluralKebab:  pluralKebab,
		TableName:    pluralSnake,
		TableAlias:   tableAlias,
		TagName:      tag,
		ChineseName:  chinese,
		RoutePath:    routePath,
		IsAdmin:      opts.IsAdmin,
	}
}

// FilePlan 描述一个待生成的目标文件与模板对应关系。
type FilePlan struct {
	RelPath  string
	Template string
}

// Plan 生成指定模块全部文件的物理目标规划。
func Plan(opts GeneratorOptions, data TemplateData) []FilePlan {
	featureDir := filepath.Join("internal", "features", "client", data.KebabName)
	if opts.IsAdmin {
		featureDir = filepath.Join("internal", "features", "admin", data.KebabName)
	}

	plans := []FilePlan{
		{RelPath: filepath.Join("internal", "shared", "models", data.SnakeName+".go"), Template: "model.tmpl"},
		{RelPath: filepath.Join(featureDir, "request.go"), Template: "request.tmpl"},
		{RelPath: filepath.Join(featureDir, "response.go"), Template: "response.tmpl"},
		{RelPath: filepath.Join(featureDir, "repository.go"), Template: "repository.tmpl"},
		{RelPath: filepath.Join(featureDir, "service.go"), Template: "service.tmpl"},
		{RelPath: filepath.Join(featureDir, "handler.go"), Template: "handler.tmpl"},
		{RelPath: filepath.Join(featureDir, "routes.go"), Template: "routes.tmpl"},
	}

	return plans
}

// GenerateCRUD 执行完整的 CRUD 模块代码脚手架生成。
func GenerateCRUD(opts GeneratorOptions) ([]GeneratedResult, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, errors.New("module name cannot be empty")
	}

	data := BuildTemplateData(opts)
	plans := Plan(opts, data)

	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	var results []GeneratedResult

	for _, p := range plans {
		absPath := filepath.Join(rootDir, p.RelPath)

		// 检查文件是否已存在
		exists := false
		if _, err := os.Stat(absPath); err == nil {
			exists = true
		}

		if exists && !opts.Force {
			results = append(results, GeneratedResult{
				Path:     p.RelPath,
				Template: p.Template,
				Action:   ActionSkipped,
				Message:  "file already exists, skipped (use --force to overwrite)",
			})
			continue
		}

		// 读取并渲染模板
		tmplContent, err := stubs.FS.ReadFile(p.Template)
		if err != nil {
			return results, fmt.Errorf("failed to read template %s: %w", p.Template, err)
		}

		tmpl, err := template.New(p.Template).Parse(string(tmplContent))
		if err != nil {
			return results, fmt.Errorf("failed to parse template %s: %w", p.Template, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return results, fmt.Errorf("failed to render template %s: %w", p.Template, err)
		}

		// 针对 Go 源码执行标准格式化
		formatted := buf.Bytes()
		if strings.HasSuffix(p.RelPath, ".go") {
			if f, err := format.Source(buf.Bytes()); err == nil {
				formatted = f
			}
		}

		if opts.DryRun {
			results = append(results, GeneratedResult{
				Path:     p.RelPath,
				Template: p.Template,
				Action:   ActionDryRun,
				Message:  fmt.Sprintf("would write %d bytes", len(formatted)),
			})
			continue
		}

		// 确保父目录存在。
		// 生成物是仓库源码，必须对同仓库其他开发者与 CI 可读，因此显式放宽到 0755/0644，
		// 并按用途豁免 gosec 的 G301/G306 默认权限要求。
		parentDir := filepath.Dir(absPath)
		if err := os.MkdirAll(parentDir, 0o755); err != nil { //nolint:gosec // 生成物目录需对协作者与 CI 可读
			return results, fmt.Errorf("failed to create directory %s: %w", parentDir, err)
		}

		if err := os.WriteFile(absPath, formatted, 0o644); err != nil { //nolint:gosec // 生成物源码需对协作者与 CI 可读
			return results, fmt.Errorf("failed to write file %s: %w", absPath, err)
		}

		action := ActionCreated
		msg := "successfully generated"
		if exists {
			action = ActionOverwritten
			msg = "overwritten existing file"
		}

		results = append(results, GeneratedResult{
			Path:     p.RelPath,
			Template: p.Template,
			Action:   action,
			Message:  msg,
		})
	}

	return results, nil
}

// DestroyCRUD 回退并安全清理指定 CRUD 模块的生成文件与空目录。
func DestroyCRUD(opts GeneratorOptions) ([]GeneratedResult, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return nil, errors.New("module name cannot be empty")
	}

	data := BuildTemplateData(opts)
	plans := Plan(opts, data)

	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	var results []GeneratedResult

	for _, p := range plans {
		absPath := filepath.Join(rootDir, p.RelPath)

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			results = append(results, GeneratedResult{
				Path:     p.RelPath,
				Template: p.Template,
				Action:   ActionSkipped,
				Message:  "file does not exist, skipped",
			})
			continue
		}

		if opts.DryRun {
			results = append(results, GeneratedResult{
				Path:     p.RelPath,
				Template: p.Template,
				Action:   ActionDryRun,
				Message:  "would delete file",
			})
			continue
		}

		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return results, fmt.Errorf("failed to delete file %s: %w", absPath, err)
		}

		results = append(results, GeneratedResult{
			Path:     p.RelPath,
			Template: p.Template,
			Action:   ActionDeleted,
			Message:  "successfully deleted",
		})
	}

	// 若未处于 DryRun 模式，尝试安全清理可能变空的 feature 目录
	if !opts.DryRun {
		featureDir := filepath.Join(rootDir, "internal", "features", "client", data.KebabName)
		if opts.IsAdmin {
			featureDir = filepath.Join(rootDir, "internal", "features", "admin", data.KebabName)
		}
		// os.Remove 仅当目录为空时才会删除，若含有用户自建的其他文件则保留
		_ = os.Remove(featureDir)
	}

	return results, nil
}
