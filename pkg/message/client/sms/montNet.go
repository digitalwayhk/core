package sms

import (
	"errors"
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/message/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type MontNet struct {
	Name string
	Host string
	Port uint
	User string
	Pass string
}

func NewMontNet(channel *models.MsgChannel) *MontNet {
	return &MontNet{
		Name: channel.Name,
		Port: channel.Port,
		Host: channel.Host,
		User: channel.User,
		Pass: channel.Pass,
	}
}

func (own MontNet) SendMsg(detail *types.SendDetail) (bool, error) {
	log.Println("Sending with MontNet")
	device := detail.Device.Text
	device = strings.ReplaceAll(device, " ", "")
	if strings.HasPrefix(device, "+") {
		device = utils.TrimFirstRune(device)
	}

	req, err := http.NewRequest("GET", own.Host, nil)
	if err != nil {
		return false, err
	}

	now := time.Now().Format("0102150405")
	secure := own.User + "00000000" + own.Pass + now
	md5str := utils.Md5(secure)

	content := detail.Content.Text
	params := req.URL.Query()
	params.Set("userid", own.User)
	params.Set("pwd", strings.ToLower(md5str))
	params.Set("mobile", device)
	params.Set("content", content)
	params.Set("timestamp", now)

	req.URL.RawQuery = params.Encode()

	var resp *http.Response
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func(Body io.ReadCloser) {
		closeErr := Body.Close()
		if closeErr != nil {
			log.Println("Http close body err: ", closeErr)
		}
	}(resp.Body)

	if resp.StatusCode == 200 {
		return true, nil
	}
	return false, errors.New("unknown err")
}
