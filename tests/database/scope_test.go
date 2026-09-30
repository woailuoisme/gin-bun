package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"bgin/internal/database"
)

type dummyUser struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            string `bun:"id,pk"`
	Name          string `bun:"name"`
	DeletedAt     string `bun:"deleted_at"`
	TenantID      string `bun:"tenant_id"`
}

type customSoftDeleteModel struct {
	bun.BaseModel `bun:"table:items,alias:i"`
	ArchivedAt    string `bun:"archived_at"`
}

func (m *customSoftDeleteModel) SoftDeleteColumn() string {
	return "archived_at"
}

func newTestSelect() *bun.SelectQuery {
	db := bun.NewDB(nil, pgdialect.New())
	return db.NewSelect().Model((*dummyUser)(nil))
}

func TestSoftDeleteScope_DefaultExcluded(t *testing.T) {
	ctx := context.Background()
	q := newTestSelect()

	q = database.ApplyScopes(q, database.SoftDeleteScope(ctx, "deleted_at", "u"))
	sql := q.String()

	assert.Contains(t, sql, "u.deleted_at IS NULL")
}

func TestSoftDeleteScope_WithTrashed(t *testing.T) {
	ctx := database.WithTrashed(context.Background())
	q := newTestSelect()

	q = database.ApplyScopes(q, database.SoftDeleteScope(ctx, "deleted_at", "u"))
	sql := q.String()

	assert.NotContains(t, sql, "WHERE")
	assert.NotContains(t, sql, "IS NULL")
}

func TestSoftDeleteScope_OnlyTrashed(t *testing.T) {
	ctx := database.OnlyTrashed(context.Background())
	q := newTestSelect()

	q = database.ApplyScopes(q, database.SoftDeleteScope(ctx, "deleted_at", "u"))
	sql := q.String()

	assert.Contains(t, sql, "u.deleted_at IS NOT NULL")
}

func TestModelSoftDeleteScope(t *testing.T) {
	db := bun.NewDB(nil, pgdialect.New())
	ctx := context.Background()

	m := &customSoftDeleteModel{}
	q := db.NewSelect().Model(m)

	q = database.ScopedSelect(q, database.ModelSoftDeleteScope(ctx, m, "i"))
	sql := q.String()

	assert.Contains(t, sql, "i.archived_at IS NULL")
}

func TestPaginateScope(t *testing.T) {
	t.Run("Normal pagination", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.PaginateScope(2, 15))
		sql := q.String()

		assert.Contains(t, sql, "LIMIT 15")
		assert.Contains(t, sql, "OFFSET 15")
	})

	t.Run("Zero and negative defaults", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.PaginateScope(0, 0))
		sql := q.String()

		assert.Contains(t, sql, "LIMIT 20")
		assert.NotContains(t, sql, "OFFSET")
	})

	t.Run("Exceeds max page size clamped", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.PaginateScope(1, 500, 50))
		sql := q.String()

		assert.Contains(t, sql, "LIMIT 50")
	})
}

func TestTenantScope(t *testing.T) {
	t.Run("With tenant ID", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.TenantScope("tenant-123", "u"))
		sql := q.String()

		assert.Contains(t, sql, "u.tenant_id = 'tenant-123'")
	})

	t.Run("Empty tenant ID is ignored", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.TenantScope(""))
		sql := q.String()

		assert.NotContains(t, sql, "WHERE")
	})
}

func TestOrderScope(t *testing.T) {
	allowed := map[string]string{
		"created_at": "u.created_at",
		"name":       "u.name",
	}

	t.Run("Valid ascending order", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.OrderScope("created_at", false, allowed))
		sql := q.String()

		assert.Contains(t, sql, "ORDER BY")
		assert.Contains(t, sql, "created_at")
		assert.Contains(t, sql, "ASC")
	})

	t.Run("Valid descending order", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.OrderScope("name", true, allowed))
		sql := q.String()

		assert.Contains(t, sql, "ORDER BY")
		assert.Contains(t, sql, "name")
		assert.Contains(t, sql, "DESC")
	})

	t.Run("Invalid field ignored", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.OrderScope("injected_sql; DROP TABLE users;", true, allowed))
		sql := q.String()

		assert.NotContains(t, sql, "ORDER BY")
	})
}

func TestWhereIfScope(t *testing.T) {
	t.Run("Condition true", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.WhereIfScope(true, func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("u.is_active = true")
		}))
		sql := q.String()

		assert.Contains(t, sql, "u.is_active = true")
	})

	t.Run("Condition false", func(t *testing.T) {
		q := newTestSelect()
		q = database.ApplyScopes(q, database.WhereIfScope(false, func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("u.is_active = true")
		}))
		sql := q.String()

		assert.NotContains(t, sql, "u.is_active = true")
	})
}
