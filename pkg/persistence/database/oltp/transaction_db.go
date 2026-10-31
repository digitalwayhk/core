// 本文件提供 SQLite/MySQL 的显式事务 SQL 访问，保留 GetRunDB 原有语义。
package oltp

import (
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"gorm.io/gorm"
)

// GetTransactionDB 返回当前 MySQL 事务连接；未初始化时拒绝，避免绕过事务提交。
func (m *MySQL) GetTransactionDB() (*gorm.DB, error) {
	if m == nil || !m.isTansaction || m.tx == nil {
		return nil, types.ErrTransactionDBUnavailable
	}
	return m.tx, m.tx.Error
}

// GetTransactionDB 返回当前 SQLite 事务连接；未初始化时拒绝，避免绕过事务提交。
func (s *Sqlite) GetTransactionDB() (*gorm.DB, error) {
	if s == nil || !s.isTansaction || s.tx == nil {
		return nil, types.ErrTransactionDBUnavailable
	}
	return s.tx, s.tx.Error
}
