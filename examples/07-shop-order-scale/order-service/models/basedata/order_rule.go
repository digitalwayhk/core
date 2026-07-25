// Package basedata 定义 07 订单服务共享基础资料模型。
package basedata

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// OrderRule 保存所有 order 实例共享的下单业务规则。
type OrderRule struct {
	*common.ServiceBaseModel
	RuleCode       string          `gorm:"type:varchar(191);not null;uniqueIndex" json:"ruleCode"`
	RuleName       string          `json:"ruleName"`
	MinQuantity    int             `json:"minQuantity"`
	MaxQuantity    int             `json:"maxQuantity"`
	MaxOrderAmount decimal.Decimal `json:"maxOrderAmount"`
	Enabled        bool            `gorm:"index" json:"enabled"`
	RuleRevision   int             `json:"ruleRevision"`
}

// NewOrderRule 创建默认订单规则模型。
func NewOrderRule() *OrderRule {
	return &OrderRule{
		ServiceBaseModel: common.NewServiceBaseModel(),
		RuleCode:         "default",
		RuleName:         "默认规则",
		MinQuantity:      1,
		MaxQuantity:      100,
		MaxOrderAmount:   decimal.NewFromInt(99999),
		Enabled:          true,
		RuleRevision:     1,
	}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (r *OrderRule) NewModel() {
	if r.ServiceBaseModel == nil || r.Model == nil {
		r.ServiceBaseModel = common.NewServiceBaseModel()
	}
}

// GetHash 返回订单规则的业务唯一散列。
func (r *OrderRule) GetHash() string { return utils.HashCodes(strings.TrimSpace(r.RuleCode)) }

// InsertWith 将订单规则写入指定事务。
func (r *OrderRule) InsertWith(action persistencetypes.IDataAction) error {
	if err := r.validate(); err != nil {
		return err
	}
	r.SetHashcode(r.GetHash())
	return action.Insert(r)
}

// UpdateWith 更新指定事务中的订单规则。
func (r *OrderRule) UpdateWith(action persistencetypes.IDataAction) error {
	if err := r.validate(); err != nil {
		return err
	}
	r.SetUpdatedAt(time.Now().UTC())
	r.SetHashcode(r.GetHash())
	return action.Update(r)
}

func (r *OrderRule) validate() error {
	if strings.TrimSpace(r.RuleCode) == "" || r.MinQuantity <= 0 || r.MaxQuantity < r.MinQuantity || r.RuleRevision <= 0 {
		return errors.New("订单规则参数不完整")
	}
	if !r.MaxOrderAmount.IsPositive() {
		return errors.New("订单最大金额必须大于 0")
	}
	return nil
}
