package database

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/samber/lo"
	"github.com/uptrace/bun"
)

// sanitizeOperator 过滤并规范化比较操作符，杜绝非法 SQL 注入。
func sanitizeOperator(op string) string {
	switch clean := strings.TrimSpace(op); clean {
	case "=", ">", ">=", "<", "<=", "!=", "<>":
		return clean
	default:
		return ">="
	}
}

// sanitizeIdent 清理表名与列名中的特殊字符，保留标准标识符字符。
func sanitizeIdent(name string) string {
	trimmed := strings.TrimSpace(name)
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			return r
		}
		return -1
	}, trimmed)
}

// With 声明关联预加载（Eager Loading），对标 Laravel with()，内部无缝接入 Bun 的关系加载管线。
func (q *Query[T]) With(relation string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) *Query[T] {
	q.sel = q.sel.Relation(relation, apply...)
	return q
}

// WithRelations 声明批量关联预加载（支持同时指定多个关联），对标 Laravel with(['a', 'b'])。
func (q *Query[T]) WithRelations(relations ...string) *Query[T] {
	for _, rel := range relations {
		if rel != "" {
			q.sel = q.sel.Relation(rel)
		}
	}
	return q
}

// WithAggregate 声明通用的关联列聚合子查询投影。
func (q *Query[T]) WithAggregate(relationTable, foreignKey, aggExpr, alias string) *Query[T] {
	table := sanitizeIdent(relationTable)
	fk := sanitizeIdent(foreignKey)
	sub := fmt.Sprintf("(SELECT %s FROM %s WHERE %s.%s = ?TableAlias.id) AS ?",
		aggExpr, table, table, fk)
	return q.SelectExpr(sub, bun.Ident(alias))
}

// WithCount 声明通过关联子查询为当前模型追加关联数量统计（对标 Laravel withCount）。
// 例如: WithCount("accounts", "user_id") 生成 (SELECT count(*) FROM accounts WHERE accounts.user_id = ?TableAlias.id) AS accounts_count
func (q *Query[T]) WithCount(relationTable, foreignKey string, alias ...string) *Query[T] {
	tbl := sanitizeIdent(relationTable)
	return q.WithAggregate(tbl, foreignKey, "count(*)", defaultString(tbl+"_count", alias))
}

// WithSum 声明通过关联子查询为当前模型追加关联列求和统计（对标 Laravel withSum）。
func (q *Query[T]) WithSum(relationTable, foreignKey, column string, alias ...string) *Query[T] {
	tbl := sanitizeIdent(relationTable)
	col := sanitizeIdent(column)
	return q.WithAggregate(tbl, foreignKey, "COALESCE(SUM("+col+"), 0)", defaultString(tbl+"_"+col+"_sum", alias))
}

// WithAvg 声明通过关联子查询为当前模型追加关联列平均值统计（对标 Laravel withAvg）。
func (q *Query[T]) WithAvg(relationTable, foreignKey, column string, alias ...string) *Query[T] {
	tbl := sanitizeIdent(relationTable)
	col := sanitizeIdent(column)
	return q.WithAggregate(tbl, foreignKey, "COALESCE(AVG("+col+"), 0)", defaultString(tbl+"_"+col+"_avg", alias))
}

// WithMin 声明通过关联子查询为当前模型追加关联列最小值统计（对标 Laravel withMin）。
func (q *Query[T]) WithMin(relationTable, foreignKey, column string, alias ...string) *Query[T] {
	tbl := sanitizeIdent(relationTable)
	col := sanitizeIdent(column)
	return q.WithAggregate(tbl, foreignKey, "MIN("+col+")", defaultString(tbl+"_"+col+"_min", alias))
}

// WithMax 声明通过关联子查询为当前模型追加关联列最大值统计（对标 Laravel withMax）。
func (q *Query[T]) WithMax(relationTable, foreignKey, column string, alias ...string) *Query[T] {
	tbl := sanitizeIdent(relationTable)
	col := sanitizeIdent(column)
	return q.WithAggregate(tbl, foreignKey, "MAX("+col+")", defaultString(tbl+"_"+col+"_max", alias))
}

func buildHasExpr(relationTable, foreignKey, operator string) string {
	table := sanitizeIdent(relationTable)
	fk := sanitizeIdent(foreignKey)
	op := sanitizeOperator(operator)
	return fmt.Sprintf("(SELECT count(*) FROM %s WHERE %s.%s = ?TableAlias.id) %s ?", table, table, fk, op)
}

// Has 检查关联模型数量是否满足指定比较操作符（对标 Laravel has）。
// 例如: Has("accounts", "user_id", ">=", 2)
func (q *Query[T]) Has(relationTable, foreignKey string, operator string, count int) *Query[T] {
	return q.Where(buildHasExpr(relationTable, foreignKey, operator), count)
}

// OrHas 增加 OR 连接的关联模型数量条件（对标 Laravel orHas）。
func (q *Query[T]) OrHas(relationTable, foreignKey string, operator string, count int) *Query[T] {
	return q.WhereOr(buildHasExpr(relationTable, foreignKey, operator), count)
}

// DoesntHave 检查关联模型数量是否为 0（对标 Laravel doesntHave）。
func (q *Query[T]) DoesntHave(relationTable, foreignKey string) *Query[T] {
	return q.Has(relationTable, foreignKey, "<", 1)
}

// OrDoesntHave 增加 OR 连接的无关联记录条件（对标 Laravel orDoesntHave）。
func (q *Query[T]) OrDoesntHave(relationTable, foreignKey string) *Query[T] {
	return q.OrHas(relationTable, foreignKey, "<", 1)
}

func (q *Query[T]) buildRelationSubquery(relationTable, foreignKey string, apply []func(*bun.SelectQuery) *bun.SelectQuery) *bun.SelectQuery {
	table := sanitizeIdent(relationTable)
	fk := sanitizeIdent(foreignKey)
	sub := q.db.NewSelect().Table(table).ColumnExpr("1").
		Where(fmt.Sprintf("%s.%s = ?TableAlias.id", table, fk))
	if len(apply) > 0 && apply[0] != nil {
		sub = apply[0](sub)
	}
	return sub
}

// WhereHasRelation 基于指定关联表和外键构建关联存在的 EXISTS 子查询过滤（支持追加关联约束闭包）。
func (q *Query[T]) WhereHasRelation(relationTable, foreignKey string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) *Query[T] {
	return q.WhereExists(q.buildRelationSubquery(relationTable, foreignKey, apply))
}

// OrWhereHasRelation 增加 OR 连接的关联存在过滤。
func (q *Query[T]) OrWhereHasRelation(relationTable, foreignKey string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) *Query[T] {
	return q.OrWhereExists(q.buildRelationSubquery(relationTable, foreignKey, apply))
}

// WhereDoesntHaveRelation 检查关联表不存在满足条件的记录（NOT EXISTS）。
func (q *Query[T]) WhereDoesntHaveRelation(relationTable, foreignKey string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) *Query[T] {
	return q.WhereNotExists(q.buildRelationSubquery(relationTable, foreignKey, apply))
}

// OrWhereDoesntHaveRelation 增加 OR 连接的关联不存在过滤。
func (q *Query[T]) OrWhereDoesntHaveRelation(relationTable, foreignKey string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) *Query[T] {
	return q.OrWhereNotExists(q.buildRelationSubquery(relationTable, foreignKey, apply))
}

// WhereHas 基于关联子查询生成 EXISTS 过滤，对标 Laravel whereHas。
func (q *Query[T]) WhereHas(builderFn func(db bun.IDB) *bun.SelectQuery) *Query[T] {
	if builderFn != nil {
		return q.WhereExists(builderFn(q.db))
	}
	return q
}

// WhereDoesntHave 基于关联子查询生成 NOT EXISTS 过滤，对标 Laravel whereDoesntHave。
func (q *Query[T]) WhereDoesntHave(builderFn func(db bun.IDB) *bun.SelectQuery) *Query[T] {
	if builderFn != nil {
		return q.WhereNotExists(builderFn(q.db))
	}
	return q
}

// Load 仓储算子：将关联关系延迟预加载到已有实体模型中（对标 Laravel $query->load()），以 1 条额外 SQL 完成整批关联加载。
func (q *Query[T]) Load(ctx context.Context, models any, relation string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) error {
	return Load(ctx, q.resolveDB(ctx), models, relation, apply...)
}

// prepareLoadTarget 校验并规范化待加载关联的目标实体（指针、切片或切片指针）。
func prepareLoadTarget(models any) (any, bool) {
	if models == nil {
		return nil, true
	}
	val := reflect.ValueOf(models)
	switch val.Kind() {
	case reflect.Pointer:
		if val.IsNil() || (val.Elem().Kind() == reflect.Slice && val.Elem().Len() == 0) {
			return nil, true
		}
		return models, false
	case reflect.Slice:
		if val.Len() == 0 {
			return nil, true
		}
		slicePtr := reflect.New(val.Type())
		slicePtr.Elem().Set(val)
		return slicePtr.Interface(), false
	default:
		return models, false
	}
}

// Load 将关联关系延迟预加载到已有实体模型（指针、切片或切片指针）中，消除循环单查导致的 N+1。
// 底层复用 Bun 内部 Relation 扫描管线，以 1 条额外 SQL 完成整批关联加载。
func Load(ctx context.Context, db bun.IDB, models any, relation string, apply ...func(*bun.SelectQuery) *bun.SelectQuery) error {
	if relation == "" {
		return nil
	}
	target, shouldSkip := prepareLoadTarget(models)
	if shouldSkip {
		return nil
	}
	actualDB := ResolveDB(ctx, db)
	sel := actualDB.NewSelect().Model(target).Relation(relation, apply...)
	return sel.Scan(ctx)
}

// executeInTx 辅助在事务中执行闭包：若当前连接已处于活跃事务则直接复用，否则自动开启新事务。
func executeInTx(ctx context.Context, db bun.IDB, fn func(ctx context.Context, idb bun.IDB) error) error {
	actualDB := ResolveDB(ctx, db)
	if tx, ok := actualDB.(bun.Tx); ok {
		return fn(WithDB(ctx, tx), tx)
	}
	if tx, ok := actualDB.(*bun.Tx); ok && tx != nil {
		return fn(WithDB(ctx, *tx), *tx)
	}
	if bdb := UnwrapBunDB(actualDB); bdb != nil {
		return bdb.RunInTx(ctx, nil, func(txCtx context.Context, tx bun.Tx) error {
			return fn(WithDB(txCtx, tx), tx)
		})
	}
	return fn(ctx, actualDB)
}

// cleanIDs 清理主键切片中的空白并去重。
func cleanIDs(ids []string) []string {
	return lo.Uniq(lo.Filter(ids, func(id string, _ int) bool {
		return strings.TrimSpace(id) != ""
	}))
}

// AttachPivot 向多对多关联中间表批量追加关联记录（冲突时忽略，对标 Laravel $relation->attach()）。
// 支持透传 Context 中的活跃事务。
func AttachPivot(ctx context.Context, db bun.IDB, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	if pivotTable == "" || sourceCol == "" || relatedCol == "" || sourceID == "" || len(relatedIDs) == 0 {
		return nil
	}
	actualDB := ResolveDB(ctx, db)
	cleanTable := sanitizeIdent(pivotTable)
	cleanSource := sanitizeIdent(sourceCol)
	cleanRelated := sanitizeIdent(relatedCol)

	uniqueIDs := cleanIDs(relatedIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	values := lo.Map(uniqueIDs, func(relID string, _ int) map[string]any {
		return map[string]any{
			cleanSource:  sourceID,
			cleanRelated: relID,
		}
	})

	_, err := actualDB.NewInsert().
		Model(&values).
		Table(cleanTable).
		Ignore().
		Exec(ctx)
	return err
}

// DetachPivot 从多对多关联中间表中移除关联记录（对标 Laravel $relation->detach()）。
// 若未提供 relatedIDs，则移除 sourceID 对应的所有关联记录。
func DetachPivot(ctx context.Context, db bun.IDB, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	if pivotTable == "" || sourceCol == "" || relatedCol == "" || sourceID == "" {
		return nil
	}
	actualDB := ResolveDB(ctx, db)
	cleanTable := sanitizeIdent(pivotTable)
	cleanSource := sanitizeIdent(sourceCol)
	cleanRelated := sanitizeIdent(relatedCol)

	del := actualDB.NewDelete().
		Table(cleanTable).
		Where("? = ?", bun.Ident(cleanSource), sourceID)

	validIDs := cleanIDs(relatedIDs)
	if len(validIDs) > 0 {
		del = del.Where("? IN (?)", bun.Ident(cleanRelated), bun.List(validIDs))
	}

	_, err := del.Exec(ctx)
	return err
}

// SyncPivot 同步多对多中间表关联（对标 Laravel $relation->sync()）。
// 自动计算差量：仅删除不在 relatedIDs 中的旧关联，仅插入新增的关联，已存在的保持不变。
// 全流程自动在事务中执行，确保原子一致性。
func SyncPivot(ctx context.Context, db bun.IDB, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs []string) error {
	if pivotTable == "" || sourceCol == "" || relatedCol == "" || sourceID == "" {
		return nil
	}
	cleanTable := sanitizeIdent(pivotTable)
	cleanSource := sanitizeIdent(sourceCol)
	cleanRelated := sanitizeIdent(relatedCol)

	cleanDesired := cleanIDs(relatedIDs)

	return executeInTx(ctx, db, func(txCtx context.Context, txIDB bun.IDB) error {
		var existingIDs []string
		err := txIDB.NewSelect().
			Table(cleanTable).
			Column(cleanRelated).
			Where("? = ?", bun.Ident(cleanSource), sourceID).
			Scan(txCtx, &existingIDs)
		if err != nil {
			return err
		}

		toDetach, toAttach := lo.Difference(existingIDs, cleanDesired)

		if len(toDetach) > 0 {
			if err := DetachPivot(txCtx, txIDB, cleanTable, cleanSource, cleanRelated, sourceID, toDetach...); err != nil {
				return err
			}
		}
		if len(toAttach) > 0 {
			if err := AttachPivot(txCtx, txIDB, cleanTable, cleanSource, cleanRelated, sourceID, toAttach...); err != nil {
				return err
			}
		}
		return nil
	})
}

// TogglePivot 切换多对多中间表关联状态（对标 Laravel $relation->toggle()）：
// 已存在的关联予以移除，不存在的关联予以追加。
func TogglePivot(ctx context.Context, db bun.IDB, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	if pivotTable == "" || sourceCol == "" || relatedCol == "" || sourceID == "" || len(relatedIDs) == 0 {
		return nil
	}
	cleanTable := sanitizeIdent(pivotTable)
	cleanSource := sanitizeIdent(sourceCol)
	cleanRelated := sanitizeIdent(relatedCol)

	uniqueIDs := cleanIDs(relatedIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	return executeInTx(ctx, db, func(txCtx context.Context, txIDB bun.IDB) error {
		var existingIDs []string
		err := txIDB.NewSelect().
			Table(cleanTable).
			Column(cleanRelated).
			Where("? = ?", bun.Ident(cleanSource), sourceID).
			Where("? IN (?)", bun.Ident(cleanRelated), bun.List(uniqueIDs)).
			Scan(txCtx, &existingIDs)
		if err != nil {
			return err
		}

		toAttach, _ := lo.Difference(uniqueIDs, existingIDs)
		toDetach := lo.Intersect(uniqueIDs, existingIDs)

		if len(toDetach) > 0 {
			if err := DetachPivot(txCtx, txIDB, cleanTable, cleanSource, cleanRelated, sourceID, toDetach...); err != nil {
				return err
			}
		}
		if len(toAttach) > 0 {
			if err := AttachPivot(txCtx, txIDB, cleanTable, cleanSource, cleanRelated, sourceID, toAttach...); err != nil {
				return err
			}
		}
		return nil
	})
}

// AttachPivot 向多对多关联中间表批量追加关联记录（支持透传当前上下文连接/事务）。
func (q *Query[T]) AttachPivot(ctx context.Context, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	return AttachPivot(ctx, q.resolveDB(ctx), pivotTable, sourceCol, relatedCol, sourceID, relatedIDs...)
}

// DetachPivot 从多对多关联中间表中移除关联记录。
func (q *Query[T]) DetachPivot(ctx context.Context, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	return DetachPivot(ctx, q.resolveDB(ctx), pivotTable, sourceCol, relatedCol, sourceID, relatedIDs...)
}

// SyncPivot 同步多对多中间表关联，自动计算差量增删并内嵌事务保障。
func (q *Query[T]) SyncPivot(ctx context.Context, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs []string) error {
	return SyncPivot(ctx, q.resolveDB(ctx), pivotTable, sourceCol, relatedCol, sourceID, relatedIDs)
}

// TogglePivot 切换多对多中间表关联状态。
func (q *Query[T]) TogglePivot(ctx context.Context, pivotTable, sourceCol, relatedCol, sourceID string, relatedIDs ...string) error {
	return TogglePivot(ctx, q.resolveDB(ctx), pivotTable, sourceCol, relatedCol, sourceID, relatedIDs...)
}
