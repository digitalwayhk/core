package oltp

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var mysqldsn = "%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=%ds&readTimeout=%ds&writeTimeout=%ds"

type Mysql struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         uint   `json:"port"`
	ConMax       uint   //最大连接数
	ConPool      uint   //连接池大小
	User         string `json:"user"`
	Pass         string `json:"pass"`
	db           *gorm.DB
	tx           *gorm.DB
	TimeOut      uint `json:"timeout"`
	ReadTimeOut  uint
	WriteTimeOut uint
	isTansaction bool
	tables       map[string]*TableMaster
	IsLog        bool
}

func (own *Mysql) init(data interface{}) error {
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
func NewMysql(host, user, pass string, port uint, islog bool) *Mysql {
	return &Mysql{
		Host:         host,
		Port:         port,
		ConMax:       100,
		ConPool:      20,
		User:         user,
		Pass:         pass,
		TimeOut:      10,
		ReadTimeOut:  30,
		WriteTimeOut: 60,
		IsLog:        islog,
	}
}
func (own *Mysql) GetDBName(data interface{}) error {
	if idb, ok := data.(types.IDBName); ok {
		own.Name = idb.GetRemoteDBName()
		if own.Name == "" {
			return errors.New("db name is empty")
		}
		return nil
	}
	return errors.New("db name is empty")
}
func (own *Mysql) GetDB() (*gorm.DB, error) {
	if own.db == nil {
		dsn := fmt.Sprintf(mysqldsn, own.User, own.Pass, own.Host, own.Port, own.Name, own.TimeOut, own.ReadTimeOut, own.WriteTimeOut)
		dia := mysql.Open(dsn)
		db, err := gorm.Open(dia, &gorm.Config{
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
			return nil, err
		}
		mysqldb, err := db.DB()
		if err != nil {
			return nil, err
		}
		mysqldb.SetMaxOpenConns(int(own.ConMax))
		mysqldb.SetMaxIdleConns(int(own.ConPool))
		own.db = db
	}
	return own.db, nil
}

func (own *Mysql) HasTable(model interface{}) error {
	if config.INITSERVER {
		return nil
	}
	if own.db == nil {
		db, err := own.GetDB()
		if err != nil {
			return err
		}
		own.db = db
	}
	// if own.tables == nil {
	// 	own.tables = make(map[string]*TableMaster)
	// 	var results []map[string]interface{}
	// 	tx := own.db.Raw("select * from information_schema.tables where table_schema='" + own.Name + "'").Find(&results)
	// 	if tx.Error != nil {
	// 		return tx.Error
	// 	}
	// 	if len(results) > 0 {
	// 		for _, v := range results {
	// 			name := strings.ToLower(v["TABLE_NAME"].(string))
	// 			count, _ := v["TABLE_ROWS"].(int)
	// 			avg, _ := v["AVG_ROW_LENGTH"].(int)
	// 			dl, _ := v["DATA_LENGTH"].(int)
	// 			il, _ := v["INDEX_LENGTH"].(int)
	// 			own.tables[name] = &TableMaster{
	// 				Rows:         count,
	// 				AvgRowLength: avg,
	// 				DataLength:   dl,
	// 				IndexLength:  il,
	// 			}
	// 		}
	// 	}
	// }
	name := utils.GetTypeName(model)
	if itb, ok := model.(types.IScopesTableName); ok {
		name = itb.TableName()
	}
	name = strings.ToLower(name)
	// if _, ok := own.tables[name]; ok {
	// 	return nil
	// }
	err := own.db.AutoMigrate(model)
	if err != nil {
		return err
	}
	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
		if kind == utils.Array {
			obj := reflect.New(field.Type.Elem().Elem()).Interface()
			err = own.HasTable(obj)
			if err != nil {
				fmt.Println(err)
			}
		}
	})
	// own.tables[name] = NewTableMaster(own.db)
	// if err != nil {
	// 	return err
	// }
	return nil
}
func (own *Mysql) Load(item *types.SearchItem, result interface{}) error {
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
func (own *Mysql) Transaction() {
	own.isTansaction = true
}
func (own *Mysql) Insert(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := createData(own.tx, data)
		if err != nil {
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return createData(own.db, data)
}
func (own *Mysql) Update(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := updateData(own.tx, data)
		if err != nil {
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return updateData(own.db, data)
}
func (own *Mysql) Delete(data interface{}) error {
	err := own.init(data)
	if err != nil {
		return err
	}
	if own.isTansaction {
		err := deleteData(own.tx, data)
		if err != nil {
			own.tx.Rollback()
			return err
		}
		return nil
	}
	return deleteData(own.db, data)
}
func (own *Mysql) Commit() error {
	own.isTansaction = false
	if own.tx != nil {
		own.tx.Commit()
		err := own.tx.Error
		own.tx = nil
		return err
	}
	return nil
}
func (own *Mysql) GetRunDB() interface{} {
	return own.db
}
