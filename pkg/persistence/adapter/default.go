package adapter

import (
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec/util"
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
	isSyncMap        sync.Map
	ForceSyncData    bool //是否强制同步远程数据和本地数据
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
	if own.saveType == types.LocalAndRemote {
		own.SyncData(model, idatabase)
	}
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
	if idb != nil && own.IsCreateTable {
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
		ormDb := getOrmDb(db, data)
		ormDb.Unscoped().Delete(data)
		return ormDb.Error
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
				continue
			}
			if index > 0 {
				own.asyncDoRemoteAction()
				own.remoteActionChan <- func() error {
					return db.Commit()
				}
				continue
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
			continue
		}
		if index > 0 && !isQuery {
			own.asyncDoRemoteAction()
			own.remoteActionChan <- func() error {
				return action(db)
			}
			continue
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
			continue
		}
		if index > 0 {
			own.asyncDoRemoteAction()
			own.remoteActionChan <- func() error {
				return db.Delete(data)
			}
			continue
		}
	}
	return nil
}

func (own *DefaultAdapter) SyncData(model interface{}, localDb types.IDataBase) {
	// 从panic中恢复
	defer func() {
		if e := recover(); e != nil {
			logx.Errorf("[PANIC]SyncData err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
		}
	}()

	if own.ForceSyncData {
		own.mergeLocalAndRemoteData(model, localDb)
		return
	}

	typeName := utils.GetTypeName(model)
	//已经同步
	if isSync, ok := own.isSyncMap.Load(typeName); ok {
		if isSync.(bool) {
			return
		}
	}

	own.syncLock.Lock()
	defer own.syncLock.Unlock()

	//二次确认
	if isSync, ok := own.isSyncMap.Load(typeName); ok {
		if isSync.(bool) {
			return
		}
	}

	own.mergeLocalAndRemoteData(model, localDb)
	own.isSyncMap.Store(typeName, true)
}

/*
1.只存在本地db的，同步到远程db
2.本地和远程都存在的，根据更新时间判断 ，如果本地是最新的，则同步到远程服务器，否则将远程服务器同步到本地
3.只存在远程db的，同步到本地db
*/
func (own *DefaultAdapter) mergeLocalAndRemoteData(model interface{}, localDataBase types.IDataBase) {
	rDatabase, _ := own.getRemoteDB(model, 0)
	remoteDB := getOrmDb(rDatabase, model)
	localDB := getOrmDb(localDataBase, model)
	modelType := reflect.TypeOf(model)
	modelListType := reflect.SliceOf(modelType)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		own.syncLocalDataToRemote(model, localDB, remoteDB, modelListType)
	}()

	go func() {
		defer wg.Done()
		own.syncOnlyInRemoteDataToLocal(model, localDB, remoteDB, modelListType)
	}()
	wg.Wait()
}

func (own *DefaultAdapter) syncLocalDataToRemote(model interface{}, localDB, remoteDB *gorm.DB, modelListType reflect.Type) {
	// 从panic中恢复
	defer func() {
		if e := recover(); e != nil {
			logx.Errorf("[PANIC]syncLocalToRemote err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
		}

	}()
	var pageSize int = 1000
	// 查询本地数据库中的数据总数
	var localCount int64
	localDB.Model(model).Count(&localCount)

	// 计算需要批量查询多少页数据
	totalPages := (localCount + int64(pageSize) - 1) / int64(pageSize)
	pageList := getPageList(int(totalPages))

	syncLocalToRemote := func(page int) error {
		localResult := reflect.MakeSlice(modelListType, 0, 0).Interface()
		remoteResult := reflect.MakeSlice(modelListType, 0, 0).Interface()

		// 计算批量查询的偏移量
		offset := (page - 1) * pageSize

		localDB.Order("id").Offset(offset).Limit(pageSize).Find(&localResult)
		localData := toModelList(localResult)
		localModelTypeValueMap := toModelTypeValueMap(localResult)

		remoteDB.Where("id IN (?)", getIDs(localData)).Find(&remoteResult)
		remoteDataMap := toModelMap(remoteResult)
		remoteModelTypeValueMap := toModelTypeValueMap(remoteResult)

		// 执行批量创建和批量更新
		localToSave := []types.IModel{}
		remoteToSave := []types.IModel{}
		for _, local := range localData {
			remote, exist := remoteDataMap[local.GetID()]

			if !exist {
				// 远程数据库中不存在该数据，则创建到远程数据库
				remoteToSave = append(remoteToSave, local)
			} else {
				// 比较更新时间
				if local.GetUpdatedAt().After(remote.GetUpdatedAt()) {
					// 本地数据更新时间较新，则更新到远程数据库
					remoteToSave = append(remoteToSave, local)
				} else if local.GetUpdatedAt().Before(remote.GetUpdatedAt()) {
					// 远程数据更新时间较新，则更新到本地数据库
					localToSave = append(localToSave, remote)
				}
			}
		}

		// 批量保存（包括创建和更新）
		if len(localToSave) > 0 {
			localDB.Model(model).Save(getModelList(remoteModelTypeValueMap, localToSave, modelListType))
			err := localDB.Error
			if err != nil {
				fmt.Println("syncLocalDataToRemote localDB save err :", err)
			}

		}

		if len(remoteToSave) > 0 {
			remoteDB.Model(model).Save(getModelList(localModelTypeValueMap, remoteToSave, modelListType))
			err := remoteDB.Error
			if err != nil {
				fmt.Println("syncLocalDataToRemote remoteDB save err :", err)
			}
		}
		return nil
	}

	task := utils.ConcurrencyTasks[int]{
		Params: pageList,
		Func: func(page int) (interface{}, error) {
			err := syncLocalToRemote(page)
			return nil, err
		},
		Concurrency: 8,
	}
	task.Run()
}

func (own *DefaultAdapter) syncOnlyInRemoteDataToLocal(model interface{}, localDB, remoteDB *gorm.DB, modelListType reflect.Type) {
	// 从panic中恢复
	defer func() {
		if e := recover(); e != nil {
			logx.Errorf("[PANIC]syncOnlyInRemoteDataToLocal err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
		}
	}()
	var pageSize int = 1000
	syncRemoteToLocal := func(page int) error {
		localResult := reflect.MakeSlice(modelListType, 0, 0).Interface()
		remoteResult := reflect.MakeSlice(modelListType, 0, 0).Interface()

		// 计算批量查询的偏移量
		offset := (page - 1) * pageSize

		remoteDB.Order("id").Offset(offset).Limit(pageSize).Find(&remoteResult)
		remoteData := toModelList(remoteResult)
		remoteModelTypeValueMap := toModelTypeValueMap(remoteData)

		localDB.Where("id IN (?)", getIDs(remoteData)).Find(&localResult)
		localDataMap := toModelMap(localResult)

		// 执行批量
		onlyInRemote := []types.IModel{}
		for _, remote := range remoteData {
			_, exist := localDataMap[remote.GetID()]
			if !exist {
				// 远程数据库中不存在该数据，则创建到远程数据库
				onlyInRemote = append(onlyInRemote, remote)
			}
		}

		// 批量保存
		if len(onlyInRemote) > 0 {
			localDB.Model(model).Save(getModelList(remoteModelTypeValueMap, onlyInRemote, modelListType))
			err := localDB.Error
			if err != nil {
				logx.Errorf("syncOnlyInRemoteDataToLocal err:%v", err)
				fmt.Println("syncOnlyInRemoteDataToLocal localDB save err :", err)
			}
		}
		return nil
	}
	// 查询远程数据库中的数据总数
	var remoteCount int64
	remoteDB.Model(model).Count(&remoteCount)
	// 计算需要批量查询多少页数据
	totalPages := (remoteCount + int64(pageSize) - 1) / int64(pageSize)
	pageList := getPageList(int(totalPages))
	task := utils.ConcurrencyTasks[int]{
		Params: pageList,
		Func: func(page int) (interface{}, error) {
			err := syncRemoteToLocal(page)
			return nil, err
		},
		Concurrency: 8,
	}
	task.Run()
}

func getModelList(modelTypeValueMap map[uint]interface{}, models []types.IModel, modelListType reflect.Type) interface{} {
	resultList := make([]interface{}, 0)
	for _, model := range models {
		modeTypeValue := modelTypeValueMap[model.GetID()]
		resultList = append(resultList, modeTypeValue)
	}
	return resultList
}

func toModelList(value interface{}) []types.IModel {
	modelList := make([]types.IModel, 0)
	reflectValue := reflect.Indirect(reflect.ValueOf(value))
	switch reflectValue.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < reflectValue.Len(); i++ {
			modelValue := reflectValue.Index(i).Interface()
			model, ok := modelValue.(types.IModel)
			if ok {
				modelList = append(modelList, model)
			}
		}
	}
	return modelList
}

func toModelTypeValueMap(value interface{}) map[uint]interface{} {
	modelMap := make(map[uint]interface{})
	reflectValue := reflect.Indirect(reflect.ValueOf(value))
	switch reflectValue.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < reflectValue.Len(); i++ {
			modelValue := reflectValue.Index(i).Interface()
			model, ok := modelValue.(types.IModel)
			if ok {
				modelMap[model.GetID()] = modelValue
			}
		}
	}
	return modelMap
}

func toModelMap(value interface{}) map[uint]types.IModel {
	modelList := toModelList(value)
	modelMap := make(map[uint]types.IModel)
	for _, model := range modelList {
		modelMap[model.GetID()] = model
	}
	return modelMap
}

func getIDs(data []types.IModel) []uint {
	var ids []uint
	for _, d := range data {
		ids = append(ids, d.GetID())
	}
	return ids
}

func getOrmDb(db types.IDataBase, model interface{}) *gorm.DB {
	modelDb, _ := db.GetModelDB(model)
	return modelDb.(*gorm.DB)
}

func getPageList(total int) []int {
	pageList := make([]int, 0)
	for i := 1; i <= total; i++ {
		pageList = append(pageList, i)
	}
	return pageList
}

func (own *DefaultAdapter) asyncDoRemoteAction() {
	own.once.Do(func() {
		go func() {
			for {
				select {
				case action := <-own.remoteActionChan:
					// 从panic中恢复
					defer func() {
						if e := recover(); e != nil {
							logx.Errorf("[PANIC]DoRemoteAction err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
						}
					}()

					err := action()
					logx.Errorf("asyncDoRemoteAction err:%v", err)
				}
			}
		}()
	})
}
