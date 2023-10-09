package types

import (
	"gorm.io/gorm"
	"time"
)

type DBConnectType int

var (
	//读写连接类型 要求有读写权限
	ReadAndWriteType DBConnectType = 0
	//只读连接类型 要求有读权限
	OnlyReadType DBConnectType = 1
	//管理连接类型 要求有增、改表列，创建修改索引权限
	ManageType DBConnectType = 2
)

type IDBSQL interface {
	SearchSQL() string
}
type IDBName interface {
	GetLocalDBName() string  //获取本地数据库的名称
	GetRemoteDBName() string //获取远程数据库的名称
}

type IDataBase interface {
	IDataAction
	HasTable(model interface{}) error
	GetDBName(data interface{}) error
}
type SaveType int

var (
	//本地和远程数据库存储
	LocalAndRemote SaveType = 0
	//只本地数据库存储
	OnlyLocal SaveType = 1
	//只远程数据库存储
	OnlyRemote SaveType = 2
)

type IListAdapter interface {
	GetDBAdapter() IDataAction
	SetDBAdapter(ida IDataAction)
	Save() error
}
type IDataAction interface {
	Transaction()
	Load(item *SearchItem, result interface{}) error
	Insert(data interface{}) error
	Update(data interface{}) error
	Delete(data interface{}) error
	Raw(sql string, data interface{}) error
	GetModelDB(model interface{}) (interface{}, error)
	Commit() error
	GetRunDB() interface{}
}
type IDataAdapter interface {
	SetSaveType(saveType SaveType)
}
type IModelNewHook interface {
	NewModel()
}
type IModelValidHook interface {
	AddValid() error
	UpdateValid(old interface{}) error
	RemoveValid() error
}

type IModelSearchHook interface {
	SearchWhere(ws []*WhereItem) ([]*WhereItem, error)
}
type IModel interface {
	Equals(o interface{}) bool
	GetID() uint
	SetID(id uint)
	GetUpdatedAt() time.Time
}
type IRowCode interface {
	GetHash() string
	SetHashcode(code string)
}
type IRowState interface {
	GetModelState() int
	SetModelState(state int)
}
type IScopes interface {
	ScopesHandler() func(*gorm.DB) *gorm.DB
}
type IScopesTableName interface {
	TableName() string
}
type ICache interface {
	Get(key string) (interface{}, error)
	Set(key string, value interface{}, seconds int) error
	Del(key string) error
	Scan() ([]string, error)
	Search(prefix string) ([]string, error)
}

type IBaseModel interface {
	GetHash() string
	IsBaseModel() bool
	SetCode(code string)
	GetCode() string
}
type IRecordModel interface {
	AddValid() error
	UpdateValid(old interface{}) error
	RemoveValid() error
	GetHash() string
}

type IOrderModel interface {
	AddValid() error
	UpdateValid(old interface{}) error
	GetHash() string
}
type IViewModel interface {
	IsView() bool
}
