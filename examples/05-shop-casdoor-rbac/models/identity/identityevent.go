package identity

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// IdentityEventRecord 保存框架已验证和标准化的 Casdoor 身份事件。
// 该模型只用于业务审计，不保存 Token、Header 或原始 Webhook。
type IdentityEventRecord struct {
	*common.BusinessModel
	EventID    string    `json:"eventID" desc:"事件 ID"`
	AuthType   string    `json:"authType" desc:"认证域"`
	UserID     string    `json:"userID" desc:"用户 ID"`
	EventType  string    `json:"eventType" desc:"事件类型"`
	Generation uint64    `json:"generation" desc:"撤销世代"`
	Blocked    bool      `json:"blocked" desc:"是否禁止访问"`
	OccurredAt time.Time `json:"occurredAt" desc:"事件发生时间"`
}

// NewIdentityEventRecord 创建已初始化完整继承链的身份事件记录。
func NewIdentityEventRecord() *IdentityEventRecord {
	return &IdentityEventRecord{BusinessModel: common.NewBusinessModel(0)}
}

// NewModel 供 ModelList 反射创建记录时初始化完整继承链。
func (own *IdentityEventRecord) NewModel() {
	if own.BusinessModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BusinessModel = common.NewBusinessModel(0)
	}
}

// Normalize 清理字段并验证审计记录的最小安全闭集。
func (own *IdentityEventRecord) Normalize() error {
	if own == nil {
		return common.NewBusinessError("身份事件不能为空")
	}
	own.EventID = strings.TrimSpace(own.EventID)
	own.AuthType = strings.TrimSpace(own.AuthType)
	own.UserID = strings.TrimSpace(own.UserID)
	own.EventType = strings.TrimSpace(own.EventType)
	switch {
	case own.EventID == "":
		return common.NewBusinessError("事件 ID 不能为空")
	case own.AuthType == "":
		return common.NewBusinessError("认证域不能为空")
	case own.UserID == "":
		return common.NewBusinessError("用户 ID 不能为空")
	case own.EventType == "":
		return common.NewBusinessError("事件类型不能为空")
	case own.OccurredAt.IsZero():
		return common.NewBusinessError("事件发生时间不能为空")
	}
	own.OccurredAt = own.OccurredAt.UTC()
	return nil
}

// GetHash 使用框架事件 ID 生成稳定幂等哈希。
func (own *IdentityEventRecord) GetHash() string {
	eventID := strings.TrimSpace(own.EventID)
	if eventID == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(eventID)
}
