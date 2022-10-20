package types

import "gorm.io/gorm"

type IModel interface {
	Equals(o interface{}) bool
	GetID() uint
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
type IBaseModel interface {
	GetHash() string
	IsBaseModel() bool
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
type IModelList interface {
}
