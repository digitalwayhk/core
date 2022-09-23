package public

import (
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"time"
)

type TgWebhook struct {
	UpdateId uint `json:"update_id"`
	*Message `json:"message"`
}

type Message struct {
	MessageId uint `json:"message_id"`
	*From     `json:"from"`
	*Chat     `json:"chat"`
	Date      time.Time `json:"date"`
	MsgText   string    `json:"text"`
}
type From struct {
	FromId       uint   `json:"id"`
	IsBot        bool   `json:"is_bot"`
	Firstname    string `json:"first_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}
type Chat struct {
	ChatId    uint   `json:"id"`
	Firstname string `json:"first_name"`
	Username  string `json:"username"`
	MsgType   string `json:"type"`
}

func (this *TgWebhook) Validation(req types.IRequest) error {
	// TODO implement me
	if this.UpdateId <= 0 && this.Message == nil {
		return errors.New("user info is empty")
	}
	return nil
}

func (this *TgWebhook) Parse(req types.IRequest) error {
	if err := req.Bind(this); err != nil {
		return err
	}
	return nil
}

func (this *TgWebhook) Do(req types.IRequest) (interface{}, error) {
	fmt.Println("TgId: ", this.FromId)
	return nil, nil
}

func (this *TgWebhook) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(this)
}
