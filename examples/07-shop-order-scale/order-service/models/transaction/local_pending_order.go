// Package transaction 定义 07 订单服务本地可靠 pending 模型。
package transaction

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// LocalPendingOrder 保存当前 order 实例尚未同步到远程权威库的订单事实。
type LocalPendingOrder struct {
	*common.RuntimeStampedModel
	OrderID    uint   `gorm:"not null;index" json:"orderID"`
	UserID     uint   `gorm:"not null;index:idx_pending_request,unique" json:"userID"`
	RequestID  string `gorm:"not null;index:idx_pending_request,unique" json:"requestID"`
	SyncStatus string `gorm:"index" json:"syncStatus"`
	RetryCount int    `json:"retryCount"`
	LastError  string `json:"lastError"`
	Payload    []byte `json:"payload"`
	SyncedAt   *time.Time
}

// NewLocalPendingOrder 创建本地 pending 订单模型。
func NewLocalPendingOrder() *LocalPendingOrder {
	return &LocalPendingOrder{RuntimeStampedModel: common.NewRuntimeStampedModel(), SyncStatus: PendingStatusAccepted}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (o *LocalPendingOrder) NewModel() {
	if o.RuntimeStampedModel == nil || o.ServiceBaseModel == nil || o.Model == nil {
		o.RuntimeStampedModel = common.NewRuntimeStampedModel()
	}
}

// GetHash 返回本地 pending 的业务唯一散列。
func (o *LocalPendingOrder) GetHash() string {
	return utils.HashCodes(strings.TrimSpace(o.RequestID), strconv.FormatUint(uint64(o.UserID), 10))
}

// InsertWith 将本地 pending 写入指定事务。
func (o *LocalPendingOrder) InsertWith(action persistencetypes.IDataAction) error {
	if o.OrderID == 0 || o.UserID == 0 || strings.TrimSpace(o.RequestID) == "" || len(o.Payload) == 0 {
		return errors.New("本地 pending 订单参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

// UpdateWith 更新指定事务中的本地 pending。
func (o *LocalPendingOrder) UpdateWith(action persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	o.SetHashcode(o.GetHash())
	return action.Update(o)
}
