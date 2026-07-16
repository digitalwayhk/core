package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const databaseName = "casdoorrbacshop"

// ShopModel 是继承商城所有模型共享的服务级基础模型。
// 它只承载持久化公共能力，不保存请求、用户或事务状态。
type ShopModel struct {
	*entity.Model
}

// NewShopModel 创建已初始化的服务级基础模型。
func NewShopModel() *ShopModel {
	return &ShopModel{Model: entity.NewModel()}
}

// GetShopModel 返回继承链中的服务级基础模型。
func (own *ShopModel) GetShopModel() *ShopModel { return own }

// GetLocalDBName 返回本示例独立使用的本地数据库名称。
func (own *ShopModel) GetLocalDBName() string { return databaseName }

// GetRemoteDBName 返回本示例对应的远端数据库名称。
func (own *ShopModel) GetRemoteDBName() string { return databaseName }
