package pdf

import (
	"context"

	"github.com/johnfercher/maroto/v2/pkg/consts/orientation"
	"github.com/johnfercher/maroto/v2/pkg/consts/pagesize"
)

// Document 是所有具体业务 PDF 文档必须实现的唯一核心契约。
type Document interface {
	// Build 编排与填充当前 PDF 文档的核心主体内容。
	Build(ctx context.Context, b *Builder) error
}

// Concern 标记可选生命周期切面接口。
type Concern any

// WithMetadata 声明 PDF 文档的标准元数据信息。
type WithMetadata interface {
	Concern
	Title() string
	Author() string
	Subject() string
	Keywords() string
}

// WithPageHeader 自定义文档跨页重复出现的统一页眉。
type WithPageHeader interface {
	Concern
	Header(ctx context.Context, b *Builder) error
}

// WithPageFooter 自定义文档跨页重复出现的统一页脚。
type WithPageFooter interface {
	Concern
	Footer(ctx context.Context, b *Builder) error
}

// WithLayout 声明自定义的页面规格、朝向与外边距。
type WithLayout interface {
	Concern
	PageSize() pagesize.Type
	Orientation() orientation.Type
	Margins() (top, right, bottom, left float64)
}

// WithDefaultPageFooter 辅助标记：声明是否自动应用平台标准企业页脚（含动态页码与时间戳）。
type WithDefaultPageFooter interface {
	Concern
	UseDefaultFooter() bool
}
