package factories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
)

// ErrPgxConnUnavailable 在底层 driver.Conn 不是 *stdlib.Conn 时返回。
var ErrPgxConnUnavailable = errors.New("factory: underlying connection is not a pgx stdlib.Conn")

// RowMapper 定义将实体对象映射为 COPY 协议值切片的函数。
type RowMapper[T any] func(item *T) []any

// CopyFromSlice 利用 pgx 原生二进制/流式 COPY 协议将切片批量写入 PostgreSQL 数据库。
// 该方法绕过标准 SQL INSERT 解析与参数序列化开销，适用于数万至百万级海量测试数据的高性能秒级播种。
func CopyFromSlice[T any](
	ctx context.Context,
	db *bun.DB,
	tableName string,
	columns []string,
	items []*T,
	mapper RowMapper[T],
) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	if db == nil {
		return 0, ErrDatabaseRequired
	}

	sqlConn, err := db.DB.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("factory copy-from get db conn failed: %w", err)
	}
	defer func() { _ = sqlConn.Close() }()

	var affected int64
	rawErr := sqlConn.Raw(func(driverConn any) error {
		stdlibConn, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return ErrPgxConnUnavailable
		}

		pgxConn := stdlibConn.Conn()
		source := pgx.CopyFromSlice(len(items), func(i int) ([]any, error) {
			return mapper(items[i]), nil
		})

		count, err := pgxConn.CopyFrom(
			ctx,
			pgx.Identifier{tableName},
			columns,
			source,
		)
		if err != nil {
			return fmt.Errorf("pgx copy-from execution failed: %w", err)
		}

		affected = count
		return nil
	})

	if rawErr != nil {
		return 0, rawErr
	}

	return affected, nil
}
