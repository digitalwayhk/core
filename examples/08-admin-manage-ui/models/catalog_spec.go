package models

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/utils"
)

// CatalogSpec 是资料条目的规格参数子行，与明细行一起验证多子表页签。
type CatalogSpec struct {
	*CatalogModel
	ItemID    uint   `json:"itemID" desc:"条目 ID"`
	SortNo    int    `json:"sortNo" desc:"排序"`
	SpecName  string `json:"specName" desc:"参数名"`
	SpecValue string `json:"specValue" desc:"参数值"`
}

// NewCatalogSpec 创建已初始化的规格参数行。
func NewCatalogSpec() *CatalogSpec {
	return &CatalogSpec{CatalogModel: NewCatalogModel()}
}

// NewModel 供 ModelList 反射创建规格行时初始化嵌入指针。
func (own *CatalogSpec) NewModel() {
	if own.CatalogModel == nil || own.Model == nil {
		own.CatalogModel = NewCatalogModel()
	}
}

// GetHash 以条目和参数名生成唯一哈希。
func (own *CatalogSpec) GetHash() string {
	name := strings.TrimSpace(own.SpecName)
	if own.ItemID == 0 || name == "" {
		if own.CatalogModel != nil && own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(strconv.FormatUint(uint64(own.ItemID), 10), name)
}

// AddValid 校验新增规格参数。
func (own *CatalogSpec) AddValid() error { return own.validate() }

// UpdateValid 校验修改规格参数。
func (own *CatalogSpec) UpdateValid(interface{}) error { return own.validate() }

func (own *CatalogSpec) validate() error {
	own.SpecName = strings.TrimSpace(own.SpecName)
	own.SpecValue = strings.TrimSpace(own.SpecValue)
	if own.ItemID == 0 {
		return NewValidationError("规格必须属于一条资料条目")
	}
	if own.SpecName == "" {
		return NewValidationError("参数名不能为空")
	}
	return nil
}
