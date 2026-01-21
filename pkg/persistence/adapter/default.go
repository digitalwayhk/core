package adapter

import (
	"errors"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/models"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	globalSqliteInstances = make(map[string]*oltp.Sqlite)
	sqliteInstanceMutex   = sync.RWMutex{}
)

func GetGlobalSqliteInstance(name string) *oltp.Sqlite {
	sqliteInstanceMutex.RLock()
	if instance, exists := globalSqliteInstances[name]; exists {
		sqliteInstanceMutex.RUnlock()
		return instance
	}
	sqliteInstanceMutex.RUnlock()

	sqliteInstanceMutex.Lock()
	defer sqliteInstanceMutex.Unlock()

	// 双重检查
	if instance, exists := globalSqliteInstances[name]; exists {
		return instance
	}

	// 创建新实例
	logx.Infof("🆕 创建全局Sqlite实例: %s", name)
	instance := oltp.NewSqlite()
	instance.Name = name
	globalSqliteInstances[name] = instance

	return instance
}

type DefaultAdapter struct {
	isTansaction  bool                       //是否开启事务
	localdbs      map[string]types.IDataBase //当前操作的数据库集合
	readDBs       map[string]types.IDataBase //读数据库
	writeDB       map[string]types.IDataBase //写数据库
	manageDB      map[string]types.IDataBase //管理数据库
	saveType      types.SaveType             //保存类型
	IsCreateTable bool                       //是否创建表,该参数只能远程库有效，当为true时，表不存在,会获取ManageType连接，创建表或者修改表结构增加列
	currentDB     []types.IDataBase          //当前操作的数据库
	IsLog         bool                       //是否打印日志
}

var defaultAda *DefaultAdapter

func NewDefaultAdapter() *DefaultAdapter {
	if defaultAda == nil {
		defaultAda = &DefaultAdapter{
			isTansaction: false,
			localdbs:     make(map[string]types.IDataBase),
			readDBs:      make(map[string]types.IDataBase),
			writeDB:      make(map[string]types.IDataBase),
			manageDB:     make(map[string]types.IDataBase),
			saveType:     0,
			IsLog:        false,
		}
	}
	return defaultAda
}

func NewCustomAdapter(saveType int) *DefaultAdapter {
	return &DefaultAdapter{
		isTansaction:  false,
		localdbs:      make(map[string]types.IDataBase),
		readDBs:       make(map[string]types.IDataBase),
		writeDB:       make(map[string]types.IDataBase),
		manageDB:      make(map[string]types.IDataBase),
		IsCreateTable: true,
		saveType:      types.SaveType(saveType),
	}
}

func getIDBName(item interface{}) (types.IDBName, error) {
	if idb, ok := item.(types.IDBName); ok {
		return idb, nil
	}
	return nil, errors.New("item is not IDBName")
}
func GetDefalueLocalDB(name string) types.IDataBase {
	if db, exists := defaultAda.localdbs[name]; exists {
		return db
	}
	sl := oltp.NewSqlite()
	sl.IsLog = defaultAda.IsLog
	sl.Name = name
	defaultAda.localdbs[name] = sl

	//logx.Infof("创建默认SQLite实例: %s", name)
	return sl
}
func (own *DefaultAdapter) getLocalDB(model interface{}) (types.IDataBase, error) {
	if utils.IsArray(model) {
		model = utils.NewArrayItem(model)
	}
	idb, err := getIDBName(model)
	if err != nil {
		return nil, err
	}
	name := idb.GetLocalDBName()

	// 🔧 使用全局实例而不是创建新的
	instance := GetGlobalSqliteInstance(name)

	// 🔧 只在第一次时检查表
	if _, exists := own.localdbs[name]; !exists {
		if !config.INITSERVER {
			err = instance.HasTable(model)
			if err != nil {
				return nil, err
			}
		}
		own.localdbs[name] = instance
		logx.Infof("🔗 绑定全局Sqlite实例到适配器: %s", name)
	}

	return instance, err
}
func (own *DefaultAdapter) getMapDB(name string, conncettype types.DBConnectType) (types.IDataBase, error) {
	if conncettype == types.ReadAndWriteType {
		if idb, ok := own.writeDB[name]; ok {
			return idb, nil
		}
	}
	if conncettype == types.OnlyReadType {
		if idb, ok := own.readDBs[name]; ok {
			return idb, nil
		}
	}
	if conncettype == types.ManageType {
		if idb, ok := own.manageDB[name]; ok {
			return idb, nil
		}
	}
	return nil, nil
}

func (own *DefaultAdapter) getRemoteDB(model interface{}, connecttype types.DBConnectType) (types.IDataBase, error) {
	if utils.IsArray(model) {
		model = utils.NewArrayItem(model)
	}
	idbn, err := getIDBName(model)
	if err != nil {
		return nil, err
	}
	name := idbn.GetRemoteDBName()
	db, err := own.getMapDB(name, connecttype)
	if err != nil {
		return nil, err
	}
	if db != nil {
		return db, nil
	}
	idb, err := models.GetConfigRemoteDB(name, connecttype, own.IsLog, true)
	if err != nil {
		return nil, err
	}
	if idb == nil && own.IsCreateTable {
		err = idb.HasTable(model)
		if err != nil {
			midb, err := models.GetConfigRemoteDB(name, types.ManageType, own.IsLog, true)
			if err != nil {
				err = midb.HasTable(model)
				return nil, err
			}
		}
	}
	if connecttype == types.ReadAndWriteType {
		own.writeDB[name] = idb
		return idb, nil
	}
	if connecttype == types.OnlyReadType {
		own.readDBs[name] = idb
		return idb, nil
	}
	if connecttype == types.ManageType {
		own.manageDB[name] = idb
		return idb, nil
	}
	return db, nil
}

func (own *DefaultAdapter) Load(item *types.SearchItem, result interface{}) error {
	var err error
	own.currentDB, err = own.getdb(item.Model)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Load(item, result)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) Raw(sql string, data interface{}) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Raw(sql, data)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) Exec(sql string, data interface{}) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Exec(sql, data)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) GetModelDB(model interface{}) (interface{}, error) {
	return own.getdb(model)
}
func (own *DefaultAdapter) Transaction() error {
	own.isTansaction = true
	return nil
}

func (own *DefaultAdapter) Insert(data interface{}) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Insert(data)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) Update(data interface{}) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Update(data)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) Delete(data interface{}) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for _, db := range own.currentDB {
		err = db.Delete(data)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *DefaultAdapter) getdb(data interface{}) ([]types.IDataBase, error) {
	dbs := make([]types.IDataBase, 0)
	if own.saveType == types.OnlyLocal || own.saveType == types.LocalAndRemote {
		ldb, err := own.getLocalDB(data)
		if err != nil {
			return nil, err
		}
		if own.isTansaction {
			ldb.Transaction()
		}
		dbs = append(dbs, ldb)
		if own.saveType == types.OnlyLocal {
			return dbs, nil
		}
	}
	if own.saveType == types.OnlyRemote || own.saveType == types.LocalAndRemote {
		rdb, err := own.getRemoteDB(data, 0)
		if err != nil {
			return nil, err
		}
		if own.isTansaction {
			rdb.Transaction()
		}
		dbs = append(dbs, rdb)
		if own.saveType == types.OnlyRemote {
			return dbs, nil
		}
	}
	return dbs, nil
}
func (own *DefaultAdapter) Commit() error {
	if own.isTansaction {
		own.isTansaction = false
		for _, db := range own.currentDB {
			err := db.Commit()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (own *DefaultAdapter) Rollback() error {
	if own.isTansaction {
		own.isTansaction = false
		for _, db := range own.currentDB {
			err := db.Rollback()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (own *DefaultAdapter) SetSaveType(saveType types.SaveType) {
	own.saveType = saveType
}

func (own *DefaultAdapter) GetRunDB() interface{} {
	return own.currentDB
}
