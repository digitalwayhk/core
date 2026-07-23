// Package transaction 定义 07 订单服务标准 EventBridge Outbox 模型。
package transaction

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// OutboxRecord 保存等待 ServiceEventBridge 发布的订单事件。
type OutboxRecord struct {
	*common.RuntimeStampedModel
	EventID     string     `gorm:"type:varchar(191);not null;uniqueIndex" json:"eventID"`
	EventType   string     `gorm:"index" json:"eventType"`
	Subject     string     `gorm:"index" json:"subject"`
	Payload     []byte     `json:"payload"`
	Published   bool       `gorm:"index" json:"published"`
	PublishedAt *time.Time `json:"publishedAt"`
}

// NewOutboxRecord 创建 Outbox 事件记录。
func NewOutboxRecord(traceID, eventID, eventType, subject string, payload interface{}) (*OutboxRecord, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	outbox := NewOutbox()
	outbox.TraceID = strings.TrimSpace(traceID)
	outbox.EventID = strings.TrimSpace(eventID)
	outbox.EventType = strings.TrimSpace(eventType)
	outbox.Subject = strings.TrimSpace(subject)
	outbox.Payload = data
	return outbox, nil
}

// NewOutbox 创建空 Outbox 事件记录。
func NewOutbox() *OutboxRecord {
	return &OutboxRecord{RuntimeStampedModel: common.NewRuntimeStampedModel()}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (o *OutboxRecord) NewModel() {
	if o.RuntimeStampedModel == nil || o.ServiceBaseModel == nil || o.Model == nil {
		o.RuntimeStampedModel = common.NewRuntimeStampedModel()
	}
}

// GetHash 返回 Outbox 事件的业务唯一散列。
func (o *OutboxRecord) GetHash() string { return utils.HashCodes(strings.TrimSpace(o.EventID)) }

// InsertWith 将 Outbox 事件写入指定事务。
func (o *OutboxRecord) InsertWith(action persistencetypes.IDataAction) error {
	if strings.TrimSpace(o.EventID) == "" || strings.TrimSpace(o.Subject) == "" || len(o.Payload) == 0 {
		return errors.New("Outbox 事件参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

// UpdateWith 更新指定事务中的 Outbox 事件。
func (o *OutboxRecord) UpdateWith(action persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	o.SetHashcode(o.GetHash())
	return action.Update(o)
}
