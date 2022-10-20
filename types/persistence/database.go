package types

type SaveType int

var (
	//本地和远程数据库存储
	LocalAndRemote SaveType = 0
	//只本地数据库存储
	OnlyLocal SaveType = 1
	//只远程数据库存储
	OnlyRemote SaveType = 2
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

type IDBName interface {
	GetLocalDBName() string  //获取本地数据库的名称
	GetRemoteDBName() string //获取远程数据库的名称
}

type IDataAction interface {
	Transaction()
	Load(item *SearchItem, result interface{}) error
	Insert(data interface{}) error
	Update(data interface{}) error
	Delete(data interface{}) error
	Commit() error
}
type IDataBase interface {
	IDataAction
	HasTable(model interface{}) error
	GetDBName(data interface{}) error
}
