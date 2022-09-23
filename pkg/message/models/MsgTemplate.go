package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"strings"
)

type MsgTemplate struct {
	*entity.Model //`json:"-"`
	//Code          uint          `json:"code"`
	Title     string        `json:"title"`
	Content   []*MsgContent `gorm:"foreignkey:TemplateId"`
	IsDisable bool          `json:"is_disable"`
}

type MsgContent struct {
	*entity.Model `json:"-"`
	Type          int    `json:"type"` // sms, email
	TemplateId    uint   `json:"template_id"`
	Details       string `json:"details"` // 驗證碼：%%  &  你已成功登錄
	FilePath      string `json:"file_path"`
	IsDisable     bool   `json:"is_disable"`
}

func (own *MsgTemplate) GetLocalDBName() string {
	return strings.ToLower(utils.GetTypeName(own))
}

func (own *MsgContent) GetLocalDBName() string {
	return strings.ToLower("MsgTemplate")
}

func NewMsgTemplate() *MsgTemplate {
	return &MsgTemplate{
		Model: entity.NewModel(),
	}
}

func (own *MsgTemplate) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

func (own *MsgContent) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
