package models

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Category 是资料目录的分类基础资料，带 Code/Name/State，供条目外键引用。
type Category struct {
	*entity.BaseModel
	Kind    int  `json:"kind" desc:"分类类型"`
	Enabled bool `json:"enabled" desc:"是否启用"`
}

// NewCategory 创建已初始化基础模型的分类。
func NewCategory() *Category {
	return &Category{BaseModel: entity.NewBaseModel()}
}

// NewModel 供 ModelList 反射创建分类时初始化基础模型。
func (own *Category) NewModel() {
	if own.BaseModel == nil || own.Model == nil {
		own.BaseModel = entity.NewBaseModel()
	}
}

// GetLocalDBName 返回本示例独立使用的本地数据库名称。
func (*Category) GetLocalDBName() string { return catalogDBName() }

// GetRemoteDBName 返回本示例对应的远端数据库名称。
func (*Category) GetRemoteDBName() string { return catalogDBName() }

// GetHash 以规范化后的稳定编码生成唯一哈希。
func (own *Category) GetHash() string {
	code := strings.ToLower(strings.TrimSpace(own.Code))
	if code == "" {
		if own.BaseModel != nil && own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}

// AddValid 校验新增分类。
func (own *Category) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改分类并排除当前记录。
func (own *Category) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 拒绝已提交或已发布的分类删除。
func (own *Category) RemoveValid() error {
	if own.BaseModel != nil {
		return own.BaseModel.RemoveValid()
	}
	return nil
}

func (own *Category) validate(excludeID uint) error {
	own.Code = strings.ToLower(strings.TrimSpace(own.Code))
	own.Name = strings.TrimSpace(own.Name)
	own.Describe = strings.TrimSpace(own.Describe)
	if own.Code == "" {
		return NewValidationError("分类编码不能为空")
	}
	if own.Name == "" {
		return NewValidationError("分类名称不能为空")
	}
	exists, err := own.CodeOrNameExists(own.Code, own.Name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return NewBusinessError("分类编码或名称不能重复")
	}
	return nil
}
