package pdf

import (
	"os"

	"github.com/johnfercher/maroto/v2/pkg/consts/fontfamily"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core/entity"
	"github.com/rs/zerolog"
)

// FontDefinition 描述自定义字体注册元数据。
type FontDefinition struct {
	Family string
	Style  fontstyle.Type
	File   string
	Bytes  []byte
}

type fontEntity struct {
	family string
	style  fontstyle.Type
	file   string
	bytes  []byte
}

func (f *fontEntity) GetFamily() string        { return f.family }
func (f *fontEntity) GetStyle() fontstyle.Type { return f.style }
func (f *fontEntity) GetFile() string          { return f.file }
func (f *fontEntity) GetBytes() []byte         { return f.bytes }

// NewCustomFontFromFile 基于本地 TTF/OTF 文件路径构造 Maroto 字体对象。
func NewCustomFontFromFile(family string, style fontstyle.Type, filePath string) entity.CustomFont {
	return &fontEntity{
		family: family,
		style:  style,
		file:   filePath,
	}
}

// NewCustomFontFromBytes 基于内存字节流构造 Maroto 字体对象。
func NewCustomFontFromBytes(family string, style fontstyle.Type, data []byte) entity.CustomFont {
	return &fontEntity{
		family: family,
		style:  style,
		bytes:  data,
	}
}

// ResolveFontConfig 解析并加载字体配置，支持本地文件、内存字节与自动回退。
func ResolveFontConfig(fontPath, fontFamily string, log *zerolog.Logger) ([]entity.CustomFont, string) {
	if fontFamily == "" {
		fontFamily = "SourceHanSans"
	}

	if fontPath == "" {
		return nil, fontfamily.Helvetica
	}

	info, err := os.Stat(fontPath)
	if err != nil || info.IsDir() {
		if log != nil {
			log.Warn().
				Str("family", fontFamily).
				Str("path", fontPath).
				Msg("PDF custom font file not found, falling back to core Helvetica font")
		}
		return nil, fontfamily.Helvetica
	}

	if log != nil {
		log.Info().
			Str("family", fontFamily).
			Str("path", fontPath).
			Msg("loaded custom PDF font successfully")
	}

	// 注册 Regular / Bold 等基础变体（若单文件则映射到主要样式）
	fonts := []entity.CustomFont{
		NewCustomFontFromFile(fontFamily, fontstyle.Normal, fontPath),
		NewCustomFontFromFile(fontFamily, fontstyle.Bold, fontPath),
		NewCustomFontFromFile(fontFamily, fontstyle.Italic, fontPath),
		NewCustomFontFromFile(fontFamily, fontstyle.BoldItalic, fontPath),
	}
	return fonts, fontFamily
}
