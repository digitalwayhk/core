package models

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// CatalogLine 是资料条目的明细子行，供管理界面展开子表和表单内增删改。
type CatalogLine struct {
	*CatalogModel
	ItemID   uint            `json:"itemID" desc:"条目 ID"`
	LineNo   int             `json:"lineNo" desc:"行号"`
	Name     string          `json:"name" desc:"明细名称"`
	Quantity int             `json:"quantity" desc:"数量"`
	Amount   decimal.Decimal `json:"amount" desc:"金额"`
}

// NewCatalogLine 创建已初始化的明细行。
func NewCatalogLine() *CatalogLine {
	return &CatalogLine{CatalogModel: NewCatalogModel()}
}

// NewModel 供 ModelList 反射创建明细行时初始化嵌入指针。
func (own *CatalogLine) NewModel() {
	if own.CatalogModel == nil || own.Model == nil {
		own.CatalogModel = NewCatalogModel()
	}
}

// GetHash 以条目、行号和名称生成唯一哈希。
func (own *CatalogLine) GetHash() string {
	name := strings.TrimSpace(own.Name)
	if own.ItemID == 0 || own.LineNo == 0 || name == "" {
		if own.CatalogModel != nil && own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(strconv.FormatUint(uint64(own.ItemID), 10), strconv.Itoa(own.LineNo), name)
}

// AddValid 校验新增明细行。
func (own *CatalogLine) AddValid() error { return own.validate() }

// UpdateValid 校验修改明细行。
func (own *CatalogLine) UpdateValid(interface{}) error { return own.validate() }

func (own *CatalogLine) validate() error {
	own.Name = strings.TrimSpace(own.Name)
	if own.ItemID == 0 {
		return NewValidationError("明细必须属于一条资料条目")
	}
	if own.Name == "" {
		return NewValidationError("明细名称不能为空")
	}
	if own.Quantity <= 0 {
		return NewValidationError("明细数量必须大于 0")
	}
	if own.Amount.IsNegative() {
		return NewValidationError("明细金额不能为负数")
	}
	return nil
}
