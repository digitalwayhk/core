package private

// import (
// 	"github.com/digitalwayhk/core/pkg/persistence/models"

// 	"errors"

// 	"github.com/digitalwayhk/core/pkg/server/api"
// 	st "github.com/digitalwayhk/core/pkg/server/types"
// )

// type ModifyRemoteDBConfig struct {
// 	*models.RemoteDbConfig
// }

// func (own *ModifyRemoteDBConfig) Parse(req st.IRequest) error {
// 	// list := models.NewRemoteDbConfigList(false)
// 	// own.RemoteDbConfig = list.NewItem()
// 	// err := req.Bind(own.RemoteDbConfig)
// 	return nil
// }
// func (own *ModifyRemoteDBConfig) Validation(req st.IRequest) error {
// 	if own.Name == "" {
// 		return errors.New("name is empty")
// 	}
// 	if own.Host == "" {
// 		return errors.New("host is empty")
// 	}
// 	if own.User == "" {
// 		return errors.New("user is empty")
// 	}
// 	if own.Pass == "" {
// 		return errors.New("pass is empty")
// 	}
// 	if own.Port == 0 {
// 		own.Port = 3306
// 	}
// 	if own.ConnectType < 0 || own.ConnectType > 2 {
// 		return errors.New("connecttype is error,only 0:ReadAndWriteType,1:OnlyReadType,2:ManageType")
// 	}
// 	return nil
// }

// func (own *ModifyRemoteDBConfig) Do(req st.IRequest) (interface{}, error) {
// 	list := models.NewRemoteDbConfigList(false)
// 	list.Clear()
// 	list.SearchName(own.Name)
// 	if list.Count() == 1 {
// 		item := list.ToArray()[0]
// 		if item.Host == "" || item.User == "" || item.Pass == "" {
// 			item.Host = own.Host
// 			item.User = own.User
// 			item.Pass = own.Pass
// 			item.Port = own.Port
// 			item.ConMax = own.ConMax
// 			item.ConPool = own.ConPool
// 			item.TimeOut = own.TimeOut
// 			item.ReadTimeOut = own.ReadTimeOut
// 			item.WriteTimeOut = own.WriteTimeOut
// 			err := list.Update(item)
// 			if err != nil {
// 				return nil, err
// 			}
// 			return item, list.Save()
// 		}
// 	}
// 	if ok, id := list.Contains(own.RemoteDbConfig); ok {
// 		own.RemoteDbConfig.ID = id
// 		list.Update(own.RemoteDbConfig)
// 	} else {
// 		list.Add(own.RemoteDbConfig)
// 	}
// 	models.TempRemoteDB = nil
// 	return own.RemoteDbConfig, list.Save()
// }

// func (own *ModifyRemoteDBConfig) RouterInfo() *st.RouterInfo {
// 	return api.ServerRouterInfo(own)
// }
