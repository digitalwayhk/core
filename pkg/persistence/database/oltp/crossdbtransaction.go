package oltp

import (
	"errors"

	"gorm.io/gorm"
)

// 跨库事务管理器
type CrossDBTransaction struct {
	mainDB      *Sqlite
	attachedDBs map[string]string // alias -> path
	tx          *gorm.DB
}

func NewCrossDBTransaction(mainDBPath string) *CrossDBTransaction {
	return &CrossDBTransaction{
		mainDB:      NewSqlite(),
		attachedDBs: make(map[string]string),
	}
}

func (cdt *CrossDBTransaction) AttachDB(alias, dbPath string) error {
	// 初始化主数据库
	if err := cdt.mainDB.init(&struct{ name string }{name: "main"}); err != nil {
		return err
	}

	// 附加其他数据库
	if err := cdt.mainDB.AttachDatabase(alias, dbPath); err != nil {
		return err
	}

	cdt.attachedDBs[alias] = dbPath
	return nil
}

func (cdt *CrossDBTransaction) Begin() error {
	if cdt.mainDB.db == nil {
		return errors.New("main database not initialized")
	}

	cdt.tx = cdt.mainDB.db.Begin()
	return cdt.tx.Error
}

func (cdt *CrossDBTransaction) Commit() error {
	if cdt.tx == nil {
		return errors.New("no active transaction")
	}

	err := cdt.tx.Commit().Error
	cdt.cleanup()
	return err
}

func (cdt *CrossDBTransaction) Rollback() error {
	if cdt.tx == nil {
		return errors.New("no active transaction")
	}

	err := cdt.tx.Rollback().Error
	cdt.cleanup()
	return err
}

func (cdt *CrossDBTransaction) cleanup() {
	// 分离附加的数据库
	for alias := range cdt.attachedDBs {
		cdt.mainDB.DetachDatabase(alias)
	}
	cdt.tx = nil
}
