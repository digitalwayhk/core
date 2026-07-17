// Package models 是 User Service 模型层的兼容门面。
//
// 新代码按语义依赖 common、basedata、transaction、internal/store 或 schema；
// 根包只导出旧名称，保持 API 和测试渐进迁移。
package models

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/schema"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	UserServiceModel = common.UserServiceModel
	BaseDataModel    = common.BaseDataModel
	BusinessModel    = common.BusinessModel
	User             = basedata.User
	Address          = basedata.Address
	Inbox            = transaction.Inbox
)

var (
	NewUserServiceModel = common.NewUserServiceModel
	NewBaseDataModel    = common.NewBaseDataModel
	NewBusinessModel    = common.NewBusinessModel
	NewUser             = basedata.NewUser
	NewAddress          = basedata.NewAddress
	NewInbox            = transaction.NewInbox
	EnsureUser          = basedata.EnsureUser
	FindUser            = basedata.FindUser
	FindUserByID        = basedata.FindUserByID
	SaveUser            = basedata.SaveUser
	InsertAddress       = basedata.InsertAddress
	FindOwnedAddress    = basedata.FindOwnedAddress
	FindAddress         = basedata.FindAddress
	ListAddresses       = basedata.ListAddresses
	DeleteAddress       = basedata.DeleteAddress
	AddressDTO          = basedata.AddressDTO
	AddressSnapshot     = basedata.AddressSnapshot
	ProcessInbox        = transaction.ProcessInbox
)

func EnsureStorage() error { return schema.EnsureStorage() }

func RunTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunInTransaction(schema.EnsureStorage, operation)
}
