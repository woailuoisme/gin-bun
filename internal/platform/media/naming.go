// Package media 提供媒体文件上传、验证、处理、命名策略与 Imgproxy 防篡改签名服务。
package media

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

// HashNamingStrategy 采用 SHA-256 内容哈希策略生成不可变、自带去重特性的文件名。
type HashNamingStrategy struct {
	HashLength int // 取哈希前缀的字符长度（缺省 16 位，最大 64 位）
}

// NewHashNamingStrategy 创建指定哈希前缀截取长度的 HashNamingStrategy 策略实例。
func NewHashNamingStrategy(hashLength ...int) *HashNamingStrategy {
	length := lo.FirstOrEmpty(hashLength)
	if length <= 0 || length > 64 {
		length = 16
	}
	return &HashNamingStrategy{HashLength: length}
}

// GenerateName 计算文件内容的 SHA-256 哈希值，拼接清洗后的原始扩展名作为文件名。
func (s *HashNamingStrategy) GenerateName(_ context.Context, originalName string, content []byte) (string, string) {
	fullHash := hashContent(content)

	ext := resolveExtension(originalName, content)
	length := s.HashLength
	if length <= 0 || length > len(fullHash) {
		length = 16
	}

	shortHash := fullHash[:length]
	fileName := shortHash + ext
	return fileName, fullHash
}

// UUIDNamingStrategy 基于高单调性 UUIDv7 生成随机不冲突的文件名。
type UUIDNamingStrategy struct{}

// NewUUIDNamingStrategy 创建 UUIDNamingStrategy 策略实例。
func NewUUIDNamingStrategy() *UUIDNamingStrategy {
	return &UUIDNamingStrategy{}
}

// GenerateName 生成基于 UUIDv7 的文件名与完整内容哈希。
func (s *UUIDNamingStrategy) GenerateName(_ context.Context, originalName string, content []byte) (string, string) {
	u, _ := uuid.NewV7()
	id := u.String()
	ext := resolveExtension(originalName, content)
	fullHash := hashContent(content)

	return id + ext, fullHash
}

// SanitizedOriginalNamingStrategy 保留清洗后的原始文件名，并追加随机安全防碰撞后缀。
type SanitizedOriginalNamingStrategy struct{}

// NewSanitizedOriginalNamingStrategy 创建 SanitizedOriginalNamingStrategy 策略实例。
func NewSanitizedOriginalNamingStrategy() *SanitizedOriginalNamingStrategy {
	return &SanitizedOriginalNamingStrategy{}
}

// GenerateName 清洗原始文件名中的非法字符，并追加 6 位的十六进制随机安全后缀。
func (s *SanitizedOriginalNamingStrategy) GenerateName(_ context.Context, originalName string, content []byte) (string, string) {
	ext := resolveExtension(originalName, content)
	base := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))

	// 清洗文件名基础部分，剔除非法字符
	cleanBase := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		if unicode.IsSpace(r) {
			return '_'
		}
		return -1
	}, base)

	cleanBase = cmp.Or(strings.ToLower(strings.Trim(cleanBase, "_-")), "file")

	randomBytes := make([]byte, 3)
	_, _ = rand.Read(randomBytes)
	suffix := hex.EncodeToString(randomBytes)

	fullHash := hashContent(content)

	fileName := fmt.Sprintf("%s_%s%s", cleanBase, suffix, ext)
	return fileName, fullHash
}

// hashContent 计算字节切片的 SHA-256 哈希值并以十六进制小写字符串输出。
func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// sanitizeExtension 规范化并清洗文件扩展名（转小写、剔除控制字符、Null Byte 与路径穿越符号）。
func sanitizeExtension(name string) string {
	cleanName := strings.ReplaceAll(name, "\x00", "")
	ext := strings.ToLower(filepath.Ext(cleanName))
	ext = strings.TrimSpace(ext)
	if ext == "" || ext == "." {
		return ""
	}

	// 仅保留点号及英文字母数字，防御控制字符与路径穿越
	var b strings.Builder
	for i, r := range ext {
		if i == 0 && r == '.' {
			b.WriteRune(r)
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if res == "." {
		return ""
	}
	return res
}

// resolveExtension 规范化并清洗文件扩展名，当原始扩展名缺失且文件内容非空时基于魔数树自动补全。
func resolveExtension(originalName string, content []byte) string {
	ext := sanitizeExtension(originalName)
	if ext == "" && len(content) > 0 {
		mtype := mimetype.Detect(content)
		if mtype != nil && mtype.Extension() != "" {
			return mtype.Extension()
		}
	}
	return ext
}
