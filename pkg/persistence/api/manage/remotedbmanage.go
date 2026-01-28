package manage

// import (
// 	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
// 	"github.com/digitalwayhk/core/pkg/persistence/models"
// 	"github.com/digitalwayhk/core/pkg/server/router"
// 	"github.com/digitalwayhk/core/pkg/server/types"
// 	"github.com/digitalwayhk/core/service/manage"
// 	"github.com/digitalwayhk/core/service/manage/view"
// )

// type RemoteDBManage struct {
// 	*manage.ManageService[models.RemoteDbConfig]
// }

// func NewRemoteDBManage() *RemoteDBManage {
// 	own := &RemoteDBManage{}
// 	own.ManageService = manage.NewManageService[models.RemoteDbConfig](own)
// 	return own
// }
// func (own *RemoteDBManage) Routers() []types.IRouter {
// 	routers := make([]types.IRouter, 0)
// 	routers = append(routers, own.ManageService.View)
// 	routers = append(routers, own.ManageService.Search)
// 	routers = append(routers, own.ManageService.Add)
// 	routers = append(routers, own.ManageService.Edit)
// 	routers = append(routers, own.ManageService.Remove)
// 	routers = append(routers, NewTestConnection(own))
// 	return routers
// }

// // ViewFieldModel 该方法用于获取视图字段，用于生成视图中的界面元素，model为数据模型，包含主模型和子模型
// func (own *RemoteDBManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
// 	if field.IsFieldOrTitle("ConnectType") {
// 		field.ComBox("读写类型", "只读类型", "管理类型")
// 	}

// 	if field.IsFieldOrTitle("Pass") {
// 		field.IsPassword = true
// 	}
// 	if field.IsFieldOrTitle("Service") {
// 		items := router.GetContexts()
// 		names := make([]string, 0)
// 		for i, _ := range items {
// 			if i == "server" {
// 				continue
// 			}
// 			names = append(names, i)
// 		}
// 		field.ComBox(names...)
// 	}
// }

// type TestConnection struct {
// 	manage.Operation[models.RemoteDbConfig]
// }

// func NewTestConnection(instance interface{}) *TestConnection {
// 	return &TestConnection{
// 		Operation: manage.NewOperation[models.RemoteDbConfig](instance),
// 	}
// }
// func (own *TestConnection) New(instance interface{}) types.IRouter {
// 	if own.Operation.GetInstance() == nil {
// 		own.Operation.New(instance)
// 	}
// 	return own
// }
// func (own *TestConnection) Do(req types.IRequest) (interface{}, error) {
// 	mysql := &oltp.MySQL{
// 		Name:         own.Model.Name,
// 		Host:         own.Model.Host,
// 		Port:         own.Model.Port,
// 		ConMax:       own.Model.ConMax,
// 		ConPool:      own.Model.ConPool,
// 		User:         own.Model.User,
// 		Pass:         own.Model.Pass,
// 		TimeOut:      own.Model.TimeOut,
// 		ReadTimeOut:  own.Model.ReadTimeOut,
// 		WriteTimeOut: own.Model.WriteTimeOut,
// 	}
// 	db, err := mysql.GetDB()
// 	if err != nil {
// 		return nil, err
// 	}
// 	result := ""
// 	tx := db.Raw("select database()").First(&result)
// 	return nil, tx.Error
// }
// func (own *TestConnection) RouterInfo() *types.RouterInfo {
// 	return manage.RouterInfo(own)
// }
