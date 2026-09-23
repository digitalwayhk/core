package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const databaseName = "adminui"

// CatalogModel 是资料条目子行等无 Code/State 语义记录的服务公共模型基座。
type CatalogModel struct {
	*entity.Model
}

// NewCatalogModel 创建已初始化的服务公共模型。
func NewCatalogModel() *CatalogModel {
	return &CatalogModel{Model: entity.NewModel()}
}

// NewModel 供 ModelList 反射创建时初始化嵌入指针。
func (own *CatalogModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetLocalDBName 返回本示例独立使用的本地数据库名称。
func (*CatalogModel) GetLocalDBName() string { return databaseName }

// GetRemoteDBName 返回本示例对应的远端数据库名称。
func (*CatalogModel) GetRemoteDBName() string { return databaseName }

func catalogDBName() string { return databaseName }
