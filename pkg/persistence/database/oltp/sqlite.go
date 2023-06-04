package oltp

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/digitalwayhk/core/pkg/persistence/local"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type Sqlite struct {
	Name         string
	Size         float64 //库大小
	UpdateTime   int32   //数据最后更新时间
	Path         string  //库文件路径
	db           *gorm.DB
	tx           *gorm.DB
	isTansaction bool
	tables       map[string]*TableMaster
	IsLog        bool
}

func NewSqlite() *Sqlite {
	sql := &Sqlite{
		tables: make(map[string]*TableMaster),
	}
	return sql
}
func (own *Sqlite) init(data interface{}) error {
	if own.Name == "" {
		err := own.GetDBName(data)
		if err != nil {
			return err
		}
	}
	if own.db == nil {
		_, err := own.GetDB()
		if err != nil {
			return err
		}
	}
	if own.isTansaction {
		if own.tx == nil {
			own.tx = own.db.Begin()
		}
	}
	return own.HasTable(data)
}

func (own *Sqlite) GetDBName(data interface{}) error {
	if idb, ok := data.(types.IDBName); ok {
		own.Name = idb.GetRemoteDBName()
		if own.Name == "" {
			return errors.New("db name is empty")
		}
		return nil
	}
	return errors.New("db name is empty")
}
func (own *Sqlite) GetDB() (*gorm.DB, error) {
	key := own.Name
	if key == "" {
		key = "models"
	}
	path, err := local.GetDbPath(key)
	if err != nil {
		return nil, err
	}
	dns := path + ".ldb"
	own.Path = dns
	dia := sqlite.Open(dns)
	db, err := gorm.Open(dia, &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
			NoLowerCase:   true,
		},
	})
	if config.INITSERVER {
		db.DryRun = true
	} else {
		if own.IsLog {
			db.Logger = logger.Default.LogMode(logger.Info)
		} else {
			db.Logger = logger.Default.LogMode(logger.Error)
		}
		db.DryRun = false
	}
	if err != nil {
		logx.Error(errors.New("数据库连接失败,path:"+dns), err)
	}
	own.db = db
	return db, err
}

func (own *Sqlite) HasTable(model interface{}) error {
	if config.INITSERVER || (own.db != nil && own.db.DryRun) {
		return nil
	}
	if own.db == nil {
		db, err := own.GetDB()
		if err != nil {
			return err
		}
		own.db = db
	}
	// name := utils.GetTypeName(model)
	// if itb, ok := model.(types.IScopesTableName); ok {
	// 	name = itb.TableName()
	// }
	// if _, ok := own.tables[name]; ok {
	// 	return nil
	// }
	// tnacopes := model.(types.IScopesTableName)
	// if tnacopes == nil {
	// 	own.db.Scopes(func(d *gorm.DB) *gorm.DB {
	// 		return d.Table(tnacopes.TableName())
	// 	})
	// } else {
	// 	scopes := model.(types.IScopes)
	// 	if scopes == nil {
	// 		own.db.Scopes(scopes.ScopesHandler())
	// 	}
	// }
	// name:=own.db.Statement.Table
	err := own.db.AutoMigrate(model)
	if err != nil {
		return err
	}
	// own.tables[name] = NewTableMaster(own.db)
	// if err != nil {
	// 	return err
	// }
	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
		if kind == utils.Array {
			obj := reflect.New(field.Type.Elem().Elem()).Interface()
			err = own.HasTable(obj)
			if err != nil {
				fmt.Println(err)
			}
		}
	})
	return nil
}
func (own *Sqlite) Load(item *types.SearchItem, result interface{}) error {
	err := own.init(item.Model)
	if err != nil {
		return err
	}
	if item.IsStatistical {
		return sum(own.db, item, result)
	}
	if own.isTansaction {
		return load(own.tx, item, result)
	}
	return load(own.db, item, result)
}
func (own *Sqlite) Transaction() {
	own.isTansaction = true
}
func (own *Sqlite) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := createData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return createData(own.db, data)
}
func (own *Sqlite) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := updateData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return updateData(own.db, data)
}
func (own *Sqlite) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := deleteData(own.tx, data)
		if err != nil {
			fmt.Println(own.Path)
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return deleteData(own.db, data)
}
func (own *Sqlite) Commit() error {
	own.isTansaction = false
	if own.tx != nil {
		own.tx.Commit()
		err := own.tx.Error
		own.tx = nil
		return err
	}
	return nil
}
func (own *Sqlite) GetRunDB() interface{} {
	return own.db
}
