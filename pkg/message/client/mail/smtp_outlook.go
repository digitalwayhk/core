package mail

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/message/types"
	"net/smtp"
	"strconv"
	"strings"
)

type OutlookSMTP struct {
	Name string
	Host string
	Port uint
	User string
	Pass string
}

func NewOutlookSMTP(channel *models.MsgChannel) *OutlookSMTP {
	return &OutlookSMTP{
		Name: channel.Name,
		Port: channel.Port,
		Host: channel.Host,
		User: channel.User,
		Pass: channel.Pass,
	}
}

func (own OutlookSMTP) SendMsg(detail *types.SendDetail) (bool, error) {
	hostStr := own.Host + ":" + strconv.FormatUint(uint64(own.Port), 10)

	header := make(map[string]string)
	header["Subject"] = detail.Title.Text
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/plain; charset=\"utf-8\""
	header["Content-Transfer-Encoding"] = "base64"

	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + base64.StdEncoding.EncodeToString([]byte(detail.Content.Text))

	c, err := clientTLS(own, hostStr)
	if err != nil {
		fmt.Println(err)
		return false, err
	}
	if err = c.Mail(own.User); err != nil {
		return false, err
	}
	if err = c.Rcpt(detail.Device.Text); err != nil {
		return false, err
	}
	w, err := c.Data()
	if err != nil {
		return false, err
	}
	_, err = w.Write([]byte(message))

	if err != nil {
		return false, err
	}
	err = w.Close()
	if err != nil {
		return false, err
	}
	return true, c.Quit()
}

type LoginAuth struct {
	username, password string
}

func NewLoginAuth(username, password string) smtp.Auth {
	return &LoginAuth{username, password}
}

func (a *LoginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *LoginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch strings.ToLower(string(fromServer)) {
		case "username:":
			return []byte(a.username), nil
		case "password:":
			return []byte(a.password), nil
		default:
			err := "Unknown from server: " + string(fromServer)
			return nil, errors.New(err)
		}
	}
	return nil, nil
}

func clientTLS(own OutlookSMTP, hostStr string) (*smtp.Client, error) {
	fmt.Println(own.User, own.Pass, own.Host)
	c, err := smtp.Dial(hostStr)
	if err != nil {
		return nil, err
	}

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: own.Host, InsecureSkipVerify: true}
		if err = c.StartTLS(config); err != nil {
			return nil, err
		}
	}
	auth := NewLoginAuth(own.User, own.Pass)
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err = c.Auth(auth); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

//func clientSSL(own SMTP, hostStr string) (*smtp.Client, error) {
//
//	// TLS config
//	tlsConfig := &tls.Config{
//		InsecureSkipVerify: true,
//		ServerName:         hostStr,
//	}
//	conn, err := tls.Dial("tcp", hostStr, tlsConfig)
//	if err != nil {
//		log.Println(err)
//	}
//
//	c, err := smtp.NewClient(conn, own.Host)
//	if err != nil {
//		log.Println(err)
//	}
//	auth := NewLoginAuth(own.User, own.Pass)
//	if auth != nil {
//		if err = c.Auth(auth); err != nil {
//			return nil, err
//		}
//	}
//	return c, nil
//}
