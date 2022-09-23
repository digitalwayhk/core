package service

import (
	"github.com/digitalwayhk/core/pkg/message/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"strings"
)

type MsgAdapter struct {
	isMail bool
}

func NewMsgAdapter(isMail bool) *MsgAdapter {
	return &MsgAdapter{
		isMail: isMail,
	}
}

// GetChannel 獲取手機通道
func (own MsgAdapter) GetChannel(device string) (types.IMsg, error) {

	var identifier string
	// TODO: get identifier from device
	// jackan@gmail.com   &&  +852 66442233
	if own.isMail {
		identifier = strings.Split(device, "@")[1]
	} else {
		identifier = strings.Split(device, " ")[0]
		if strings.HasPrefix(identifier, "+") {
			identifier = utils.TrimFirstRune(identifier)
		}
	}

	iMsg, err := GetMsgChannel(identifier, own.isMail)
	if err != nil {
		return nil, err
	}
	return iMsg, nil
}
