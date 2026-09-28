// Package models 定义与数据库表一一对应的 bun 模型。
// 模型不是 schema 的真相源：建表以 internal/database/schemas/ 为准（ADR-0004）。
package models

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Base 提供三张表共用的主键与时间戳，并在写入前统一补齐。
type Base struct {
	ID        string    `bun:"id,pk"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// UTCPtr 返回时间指针的 UTC 形式，nil 时返回 nil。
// pgx 把 timestamptz 解码成 time.Local（瞬时正确、偏移是服务器本地时区），
// 而 api.md 2.2 要求响应体里的时间统一为 UTC 并以 Z 结尾，因此投影时必须过一遍这里。
func UTCPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}

// BeforeAppendModel 在写入前生成 UUIDv7 并刷新时间戳，数据库默认值仅作兜底。
func (b *Base) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now().UTC()

	switch query.(type) {
	case *bun.InsertQuery:
		if b.ID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			b.ID = id.String()
		}
		b.CreatedAt = now
		b.UpdatedAt = now
	case *bun.UpdateQuery:
		b.UpdatedAt = now
	}
	return nil
}
