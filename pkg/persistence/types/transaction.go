// 本文件定义独立于 IDataAction 必选方法的事务 SQL 访问能力。
package types

import (
	"errors"

	"gorm.io/gorm"
)

// ErrTransactionDBUnavailable 表示当前 action 没有已开启的 SQL 事务连接。
var ErrTransactionDBUnavailable = errors.New("当前数据操作器没有已开启的 SQL 事务连接")

// ITransactionDB 仅暴露当前事务连接，不得回退到基础连接或自动开启另一事务。
// 调用方须先通过 IDataAction.Transaction 及模型访问完成懒事务初始化。
type ITransactionDB interface {
	GetTransactionDB() (*gorm.DB, error)
}
