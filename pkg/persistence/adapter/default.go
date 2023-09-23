package adapter

import (
	"errors"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/models"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"sync"
)

type DefaultAdapter struct {
	isTansaction     bool                       //是否开启事务
	localdbs         map[string]types.IDataBase //当前操作的数据库集合
	readDBs          map[string]types.IDataBase //读数据库
	writeDB          map[string]types.IDataBase //写数据库
	manageDB         map[string]types.IDataBase //管理数据库
	saveType         types.SaveType             //保存类型
	IsCreateTable    bool                       //是否创建表,该参数只能远程库有效，当为true时，表不存在,会获取ManageType连接，创建表或者修改表结构增加列
	currentDB        []types.IDataBase          //当前操作的数据库
	IsLog            bool                       //是否打印日志
	remoteActionChan chan func() error
	once             sync.Once
	syncLock         sync.Mutex
	isSync           bool
}

var defaultAda *DefaultAdapter

func NewDefaultAdapter() *DefaultAdapter {
	if defaultAda == nil {
		defaultAda = &DefaultAdapter{
			isTansaction:     false,
			localdbs:         make(map[string]types.IDataBase),
			readDBs:          make(map[string]types.IDataBase),
			writeDB:          make(map[string]types.IDataBase),
			manageDB:         make(map[string]types.IDataBase),
			saveType:         0,
			IsLog:            false,
			remoteActionChan: make(chan func() error),
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
	db := defaultAda.localdbs[name]
	if db == nil {
		sl := oltp.NewSqlite()
		sl.IsLog = defaultAda.IsLog
		sl.Name = name
		defaultAda.localdbs[name] = sl
	}
	return defaultAda.localdbs[name]
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
	if _, ok := own.localdbs[name]; !ok {
		ndb := oltp.NewSqlite()
		ndb.IsLog = own.IsLog
		ndb.Name = name
		if !config.INITSERVER {
			own.localdbs[name] = ndb
		} else {
			return ndb, nil
		}
	}
	idatabase := own.localdbs[name]
	if !config.INITSERVER {
		err = idatabase.HasTable(model)
	}
	own.SyncRemoteData(model, idatabase)
	return idatabase, err
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
	return own.doAction(item.Model, true, func(db types.IDataBase) error {
		return db.Load(item, result)
	})
}
func (own *DefaultAdapter) Raw(sql string, data interface{}) error {
	upperSQL := strings.ToUpper(sql)
	isQuery := strings.Contains(upperSQL, "SELECT")
	return own.doAction(data, isQuery, func(db types.IDataBase) error {
		return db.Raw(sql, data)
	})
}
func (own *DefaultAdapter) GetModelDB(model interface{}) (interface{}, error) {
	return own.getdb(model)
}
func (own *DefaultAdapter) Transaction() {
	own.isTansaction = true
}

func (own *DefaultAdapter) Insert(data interface{}) error {
	return own.doAction(data, false, func(db types.IDataBase) error {
		return db.Insert(data)
	})
}
func (own *DefaultAdapter) Update(data interface{}) error {
	return own.doAction(data, false, func(db types.IDataBase) error {
		return db.Update(data)
	})
}
func (own *DefaultAdapter) Delete(data interface{}) error {
	return own.doDeleteAction(data, func(db types.IDataBase) error {
		return db.Delete(data)
	})
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
		for index, db := range own.currentDB {
			if index == 0 {
				err := db.Commit()
				if err != nil {
					return err
				}
			}
			if index > 0 {
				own.asyncDoRemoteAction()
				own.remoteActionChan <- func() error {
					return db.Commit()
				}
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

func (own *DefaultAdapter) doAction(data interface{}, isQuery bool, action func(db types.IDataBase) error) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for index, db := range own.currentDB {
		if index == 0 {
			err = action(db)
			if err != nil {
				return err
			}
		}
		if index > 0 && !isQuery {
			own.asyncDoRemoteAction()
			own.remoteActionChan <- func() error {
				return action(db)
			}
		}
	}
	return nil
}

func (own *DefaultAdapter) doDeleteAction(data interface{}, action func(db types.IDataBase) error) error {
	var err error
	own.currentDB, err = own.getdb(data)
	if err != nil {
		return err
	}
	for index, db := range own.currentDB {
		if index == 0 {
			err = action(db)
			if err != nil {
				return err
			}
		}
		if index > 0 {
			own.asyncDoRemoteAction()
			own.remoteActionChan <- func() error {
				utils.SetPropertyValue(data, "is_delete", true)
				return db.Update(data)
			}
		}
	}
	return nil
}

func (own *DefaultAdapter) SyncRemoteData(data interface{}, localDb types.IDataBase) {
	if own.isSync {
		return
	}
	own.syncLock.Lock()
	defer own.syncLock.Unlock()

	//二次确认
	if own.isSync {
		return
	}

	sql := localDb.GetRunDB().(*gorm.DB)
	var count int64
	sql.Model(data).Count(&count)
	if count == 0 {
		own.sysRemoteDataToLocal(data, localDb)
	}

	own.isSync = true
}

func (own *DefaultAdapter) sysRemoteDataToLocal(model interface{}, localDb types.IDataBase) {
	rDatabase, _ := own.getRemoteDB(model, 0)
	modelDb, _ := rDatabase.GetModelDB(model)
	var maxId int
	sql := modelDb.(*gorm.DB)
	sql.Model(model).Select("max(id)").Scan(&maxId)
	if maxId == 0 {
		return
	}

	numIntervals := 200
	intervals := calculateIntervals(maxId, numIntervals)
	task := utils.ConcurrencyTasks[interval]{
		Params: intervals,
		Func: func(param interval) (interface{}, error) {
			modelType := reflect.TypeOf(model)
			modellistType := reflect.SliceOf(modelType)
			resultList := reflect.MakeSlice(modellistType, 0, 0).Interface()
			searchItem := &types.SearchItem{
				WhereList: []*types.WhereItem{},
				Model:     model,
				Page:      1,
				Size:      numIntervals,
			}
			searchItem.WhereList = append(searchItem.WhereList, &types.WhereItem{
				Column: "id",
				Value:  param.start,
				Symbol: ">=",
			})
			searchItem.WhereList = append(searchItem.WhereList, &types.WhereItem{
				Column: "id",
				Value:  param.end,
				Symbol: "<",
			})
			err := rDatabase.Load(searchItem, &resultList)
			err = localDb.Insert(resultList)
			if err != nil {
				logx.Errorf("sysRemoteDataToLocal err:%v", err)
			}
			return err, nil
		},
		Concurrency: 8,
	}
	task.Run()
}

type interval struct {
	start int
	end   int
}

func calculateIntervals(n, numIntervals int) []interval {
	if n < 1 || numIntervals < 1 {
		return nil
	}

	intervalSize := n / numIntervals
	remainder := n % numIntervals

	intervals := make([]interval, numIntervals)
	start := 1
	for i := 0; i < numIntervals; i++ {
		end := start + intervalSize - 1
		if remainder > 0 {
			end++
			remainder--
		}
		intervals[i] = interval{start, end}
		start = end + 1
	}

	return intervals
}

func (own *DefaultAdapter) asyncDoRemoteAction() {
	own.once.Do(func() {
		go func() {
			for {
				select {
				case action := <-own.remoteActionChan:
					err := action()
					logx.Errorf("asyncDoRemoteAction err:%v", err)
				}
			}
		}()
	})
}
