package types

import "time"

const (
	SmsMsg = iota
	MailMsg
	AppNotice
)

type IMsg interface {
	SendMsg(detail *SendDetail) (bool, error)
}

type SendDetail struct {
	Device  DeviceDetail  `json:"device"`
	Title   TitleDetail   `json:"title"`
	Content ContentDetail `json:"content"`
}

type DeviceDetail struct {
	Text string `json:"text"`
}

type TitleDetail struct {
	Text string `json:"text"`
}

type ContentDetail struct {
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

func NewSendDetail() *SendDetail {
	return &SendDetail{}
}
