package oltp

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// 基于您现有架构的多库事务管理器
type MultiDBTransaction struct {
	databases    map[string]*Sqlite
	transactions map[string]*gorm.DB
	isActive     bool
	mu           sync.RWMutex
}

func NewMultiDBTransaction() *MultiDBTransaction {
	return &MultiDBTransaction{
		databases:    make(map[string]*Sqlite),
		transactions: make(map[string]*gorm.DB),
		isActive:     false,
	}
}

func (mdt *MultiDBTransaction) AddDatabase(name string, dbPath string) error {
	mdt.mu.Lock()
	defer mdt.mu.Unlock()

	if mdt.isActive {
		return errors.New("cannot add database to active transaction")
	}

	sqlite := NewSqlite()
	sqlite.Name = name
	sqlite.Path = dbPath

	// 初始化数据库连接
	if err := sqlite.init(&struct {
		name string
	}{name: name}); err != nil {
		return err
	}

	mdt.databases[name] = sqlite
	return nil
}

func (mdt *MultiDBTransaction) Begin() error {
	mdt.mu.Lock()
	defer mdt.mu.Unlock()

	if mdt.isActive {
		return errors.New("transaction already active")
	}

	// 为每个数据库开始事务
	for name, sqlite := range mdt.databases {
		if sqlite.db == nil {
			if _, err := sqlite.GetDB(); err != nil {
				mdt.rollbackAll()
				return fmt.Errorf("failed to get DB for %s: %v", name, err)
			}
		}

		tx := sqlite.db.Begin()
		if tx.Error != nil {
			mdt.rollbackAll()
			return fmt.Errorf("failed to begin transaction for %s: %v", name, tx.Error)
		}

		mdt.transactions[name] = tx
	}

	mdt.isActive = true
	return nil
}

func (mdt *MultiDBTransaction) GetDB(name string) (*gorm.DB, error) {
	mdt.mu.RLock()
	defer mdt.mu.RUnlock()

	if !mdt.isActive {
		return nil, errors.New("no active transaction")
	}

	tx, exists := mdt.transactions[name]
	if !exists {
		return nil, fmt.Errorf("database %s not found in transaction", name)
	}

	return tx, nil
}

func (mdt *MultiDBTransaction) Commit() error {
	mdt.mu.Lock()
	defer mdt.mu.Unlock()

	if !mdt.isActive {
		return errors.New("no active transaction")
	}

	// 先检查所有事务是否可以提交
	for name, tx := range mdt.transactions {
		if tx.Error != nil {
			mdt.rollbackAll()
			return fmt.Errorf("transaction for %s has error: %v", name, tx.Error)
		}
	}

	// 提交所有事务
	var commitErrors []string
	for name, tx := range mdt.transactions {
		if err := tx.Commit().Error; err != nil {
			commitErrors = append(commitErrors, fmt.Sprintf("%s: %v", name, err))
		}
	}

	mdt.isActive = false
	mdt.transactions = make(map[string]*gorm.DB)

	if len(commitErrors) > 0 {
		return fmt.Errorf("commit failed for: %s", strings.Join(commitErrors, "; "))
	}

	return nil
}

func (mdt *MultiDBTransaction) Rollback() error {
	mdt.mu.Lock()
	defer mdt.mu.Unlock()

	return mdt.rollbackAll()
}

func (mdt *MultiDBTransaction) rollbackAll() error {
	if !mdt.isActive {
		return errors.New("no active transaction")
	}

	var rollbackErrors []string
	for name, tx := range mdt.transactions {
		if err := tx.Rollback().Error; err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", name, err))
		}
	}

	mdt.isActive = false
	mdt.transactions = make(map[string]*gorm.DB)

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("rollback failed for: %s", strings.Join(rollbackErrors, "; "))
	}

	return nil
}
