package models

import (
	"errors"
	"strconv"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

var once sync.Once

var list *entity.ModelList[RemoteDbConfig]

//临时远程库配置，修改配置文件中的Debug为false,会失效这里的任何设置
var TempRemoteDB map[string]*RemoteDbConfig

func init() {
	TempRemoteDB = make(map[string]*RemoteDbConfig)
}

var GetRemoteDBHandler func(dbconfig *RemoteDbConfig)

func GetConfigRemoteDB(name string, connecttype types.DBConnectType) (types.IDataBase, error) {
	if len(TempRemoteDB) > 0 {
		if db, ok := TempRemoteDB[name]; ok {
			return oltp.NewMysql(db.Host, db.User, db.Pass, db.Port), nil
		}
	}
	if list == nil {
		list = NewRemoteDbConfigList()
	}
	if GetRemoteDBHandler != nil {
		rdc := &RemoteDbConfig{
			Name:        name,
			ConnectType: connecttype,
		}
		GetRemoteDBHandler(rdc)
		return dbconToIdb(rdc)
	}
	dbconfig, err := list.SearchName(name)
	if err != nil {
		return nil, err
	}
	if len(dbconfig) == 0 {
		addconfig(name)
		return nil, errors.New(name + " database not set romotedb config")
	}
	rdc, err := localdbGetConfig(name, connecttype)
	if err != nil {
		return nil, errors.New(name + " database not set romotedb config type:" + strconv.Itoa(int(connecttype)))
	}
	return dbconToIdb(rdc)
}
func addconfig(name string) error {
	item := list.NewItem()
	item.Name = name
	item.ConnectType = 0

	list.Add(item)
	return list.Save()
}
func localdbGetConfig(name string, connecttype types.DBConnectType) (*RemoteDbConfig, error) {
	_, rdc := list.FindOne(func(o *RemoteDbConfig) bool {
		if connecttype == types.ReadAndWriteType {
			return o.ConnectType == types.ReadAndWriteType
		}
		if connecttype == types.OnlyReadType {
			return o.ConnectType == types.OnlyReadType || o.ConnectType == types.ReadAndWriteType
		}
		if connecttype == types.ManageType {
			return o.ConnectType == types.ManageType
		}
		return false
	})
	if rdc == nil {
		return nil, errors.New(name + " database not set romotedb config type:" + strconv.Itoa(int(connecttype)))
	}
	return rdc, nil
}

func dbconToIdb(rdc *RemoteDbConfig) (types.IDataBase, error) {
	mysql := oltp.NewMysql(rdc.Host, rdc.User, rdc.Pass, rdc.Port)
	if mysql.Host == "" || mysql.User == "" || mysql.Pass == "" || mysql.Port == 0 {
		return nil, errors.New(rdc.Name + " database not set romotedb config")
	}
	mysql.Name = rdc.Name
	if rdc.ConMax > 0 {
		mysql.ConMax = rdc.ConMax
	}
	if rdc.ConPool > 0 {
		mysql.ConPool = rdc.ConPool
	}
	if rdc.TimeOut > 0 {
		mysql.TimeOut = rdc.TimeOut
	}
	if rdc.ReadTimeOut > 0 {
		mysql.ReadTimeOut = rdc.ReadTimeOut
	}
	if rdc.WriteTimeOut > 0 {
		mysql.WriteTimeOut = rdc.WriteTimeOut
	}
	return mysql, nil
}
func NewRemoteDbConfigList() *entity.ModelList[RemoteDbConfig] {
	// if list == nil {
	// 	once.Do(func() {
	// 		list = entity.NewModelList[RemoteDbConfig](nil)
	// 	})
	// }
	return entity.NewModelList[RemoteDbConfig](nil)
}
