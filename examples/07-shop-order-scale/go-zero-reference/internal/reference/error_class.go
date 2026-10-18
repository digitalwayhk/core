// 本文件只分类错误供观测，不据此重放写事务。
package reference

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/go-sql-driver/mysql"
)

func errorClass(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) {
		return "sql_connection"
	}
	var sqlError *mysql.MySQLError
	if errors.As(err, &sqlError) {
		return "sql_statement_or_transaction"
	}
	return "operation"
}
