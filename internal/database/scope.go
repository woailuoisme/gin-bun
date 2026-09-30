// Package database 负责 PostgreSQL 连接池管理、事务工作单元与声明式查询作用域支持。
package database

import (
	"context"

	"github.com/uptrace/bun"
)

// TrashedMode 定义软删除数据的检索模式。
type TrashedMode int

const (
	// TrashedExclude 默认模式：仅查询未软删除的数据 (deleted_at IS NULL)。
	TrashedExclude TrashedMode = iota
	// TrashedInclude 包含模式：查询包含已被软删除的所有数据 (不追加 deleted_at 过滤)。
	TrashedInclude
	// TrashedOnly 仅已删除模式：仅查询已被软删除的数据 (deleted_at IS NOT NULL)。
	TrashedOnly
)

type trashedCtxKey struct{}

// WithTrashed 标记查询上下文包含已被软删除的数据。
func WithTrashed(ctx context.Context) context.Context {
	return context.WithValue(ctx, trashedCtxKey{}, TrashedInclude)
}

// OnlyTrashed 标记查询上下文仅查询已被软删除的数据。
func OnlyTrashed(ctx context.Context) context.Context {
	return context.WithValue(ctx, trashedCtxKey{}, TrashedOnly)
}

// TrashedModeFromContext 从上下文中提取软删除检索模式，默认为 TrashedExclude。
func TrashedModeFromContext(ctx context.Context) TrashedMode {
	if mode, ok := ctx.Value(trashedCtxKey{}).(TrashedMode); ok {
		return mode
	}
	return TrashedExclude
}

// SoftDeletable 声明支持软删除的模型契约。
type SoftDeletable interface {
	SoftDeleteColumn() string
}

// Scope 是针对 bun.SelectQuery 的声明式修饰函数契约。
type Scope func(*bun.SelectQuery) *bun.SelectQuery

// ApplyScopes 将一组 Scope 依次应用于目标 SelectQuery 并返回最终查询构建器。
func ApplyScopes(q *bun.SelectQuery, scopes ...Scope) *bun.SelectQuery {
	for _, scope := range scopes {
		if scope != nil {
			q = scope(q)
		}
	}
	return q
}

// ScopedSelect 是 ApplyScopes 的语义化别名，用于链式查询开端。
func ScopedSelect(q *bun.SelectQuery, scopes ...Scope) *bun.SelectQuery {
	return ApplyScopes(q, scopes...)
}

// SoftDeleteScope 根据上下文中的 TrashedMode 自动注入软删除过滤条件。
// column: 软删除时间戳列名，缺省为 "deleted_at"；
// alias: 可选表别名（如 "u" -> 生成 "u.deleted_at IS NULL"）。
func SoftDeleteScope(ctx context.Context, column string, alias ...string) Scope {
	if column == "" {
		column = "deleted_at"
	}
	colExpr := qualifyColumn(column, alias)
	mode := TrashedModeFromContext(ctx)

	return func(q *bun.SelectQuery) *bun.SelectQuery {
		switch mode {
		case TrashedExclude:
			return q.Where(colExpr + " IS NULL")
		case TrashedOnly:
			return q.Where(colExpr + " IS NOT NULL")
		case TrashedInclude:
			return q
		default:
			return q.Where(colExpr + " IS NULL")
		}
	}
}

// ModelSoftDeleteScope 针对实现了 SoftDeletable 接口的模型推导字段名并应用软删除作用域。
func ModelSoftDeleteScope(ctx context.Context, model SoftDeletable, alias ...string) Scope {
	if model == nil {
		return SoftDeleteScope(ctx, "deleted_at", alias...)
	}
	col := model.SoftDeleteColumn()
	if col == "" {
		col = "deleted_at"
	}
	return SoftDeleteScope(ctx, col, alias...)
}

// PaginateScope 统一注入 Limit 与 Offset，内置防负数与防超大页截断保护。
func PaginateScope(page, pageSize int, maxPageSize ...int) Scope {
	maxLimit := 100
	if len(maxPageSize) > 0 && maxPageSize[0] > 0 {
		maxLimit = maxPageSize[0]
	}
	if pageSize <= 0 {
		pageSize = 20
	} else if pageSize > maxLimit {
		pageSize = maxLimit
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	return func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Limit(pageSize).Offset(offset)
	}
}

// TenantScope 注入多租户隔离过滤条件。若 tenantID 为空则跳过过滤。
func TenantScope(tenantID string, alias ...string) Scope {
	colExpr := qualifyColumn("tenant_id", alias)
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		if tenantID == "" {
			return q
		}
		return q.Where(colExpr+" = ?", tenantID)
	}
}

// OrderScope 统一注入字段排序，必须通过 allowedColumns 白名单校验，彻底防范 SQL 注入。
func OrderScope(sortField string, isDesc bool, allowedColumns map[string]string) Scope {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		if sortField == "" || allowedColumns == nil {
			return q
		}
		actualCol, ok := allowedColumns[sortField]
		if !ok || actualCol == "" {
			return q
		}
		direction := "ASC"
		if isDesc {
			direction = "DESC"
		}
		return q.Order(actualCol + " " + direction)
	}
}

// WhereIfScope 当 condition 为 true 时应用目标 scope，否则保持查询不变。
func WhereIfScope(condition bool, scope Scope) Scope {
	return func(q *bun.SelectQuery) *bun.SelectQuery {
		if condition && scope != nil {
			return scope(q)
		}
		return q
	}
}

// qualifyColumn 辅助拼接表别名与列名。
func qualifyColumn(column string, alias []string) string {
	if len(alias) > 0 && alias[0] != "" {
		return alias[0] + "." + column
	}
	return column
}
