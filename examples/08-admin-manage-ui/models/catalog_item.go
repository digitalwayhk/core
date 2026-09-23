package models

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// CatalogItem 是资料目录主条目，覆盖管理界面全部字段控件、外键选择和多个子表。
type CatalogItem struct {
	*entity.BaseModel
	CategoryID  uint            `json:"categoryID" desc:"分类 ID"`
	Category    *Category       `json:"category,omitempty" gorm:"foreignkey:ID;references:CategoryID"`
	Kind        int             `json:"kind" desc:"条目类型"`
	Price       decimal.Decimal `json:"price" desc:"价格"`
	Stock       int             `json:"stock" desc:"库存"`
	Enabled     bool            `json:"enabled" desc:"是否启用"`
	Secret      string          `json:"secret" desc:"访问口令"`
	Note        string          `json:"note" desc:"备注"`
	PublishedAt *time.Time      `json:"publishedAt" desc:"上架时间"`
	Lines       []*CatalogLine  `json:"lines" gorm:"-" desc:"明细行"`
	Specs       []*CatalogSpec  `json:"specs" gorm:"-" desc:"规格参数"`
}

// NewCatalogItem 创建已初始化基础模型的资料条目。
func NewCatalogItem() *CatalogItem {
	return &CatalogItem{BaseModel: entity.NewBaseModel()}
}

// NewModel 供 ModelList 反射创建条目时初始化基础模型。
func (own *CatalogItem) NewModel() {
	if own.BaseModel == nil || own.Model == nil {
		own.BaseModel = entity.NewBaseModel()
	}
}

// GetLocalDBName 返回本示例独立使用的本地数据库名称。
func (*CatalogItem) GetLocalDBName() string { return catalogDBName() }

// GetRemoteDBName 返回本示例对应的远端数据库名称。
func (*CatalogItem) GetRemoteDBName() string { return catalogDBName() }

// IsPreload 让主查询预加载分类外键对象，供表格显示名称。
func (*CatalogItem) IsPreload() bool { return true }

// GetHash 以规范化后的稳定编码生成唯一哈希。
func (own *CatalogItem) GetHash() string {
	code := strings.ToLower(strings.TrimSpace(own.Code))
	if code == "" {
		if own.BaseModel != nil && own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}

// AddValid 校验新增资料条目。
func (own *CatalogItem) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改资料条目并排除当前记录。
func (own *CatalogItem) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 拒绝已提交或已发布的条目删除。
func (own *CatalogItem) RemoveValid() error {
	if own.BaseModel != nil {
		return own.BaseModel.RemoveValid()
	}
	return nil
}

func (own *CatalogItem) validate(excludeID uint) error {
	own.Code = strings.ToLower(strings.TrimSpace(own.Code))
	own.Name = strings.TrimSpace(own.Name)
	own.Secret = strings.TrimSpace(own.Secret)
	own.Note = strings.TrimSpace(own.Note)
	if own.Code == "" {
		return NewValidationError("条目编码不能为空")
	}
	if own.Name == "" {
		return NewValidationError("条目名称不能为空")
	}
	if own.CategoryID == 0 {
		return NewValidationError("请选择分类")
	}
	if own.Price.IsNegative() {
		return NewValidationError("价格不能为负数")
	}
	if own.Stock < 0 {
		return NewValidationError("库存不能为负数")
	}
	exists, err := own.CodeExists(own.Code, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return NewBusinessError("条目编码不能重复")
	}
	return nil
}
