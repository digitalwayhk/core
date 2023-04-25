package trans

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"
)

func VerifyIPWhiteList(con *config.ServerConfig, ip string) error {
	if con.IsWhiteList {
		okip := false
		for _, ipw := range con.WhiteList {
			if ipw == ip {
				okip = true
				break
			}
		}
		if !okip {
			return errors.New("IP地址不在白名单中")
		}
	}
	if con.IsLoaclVisit {
		if !utils.HasLocalIPAddr(ip) {
			return errors.New("只能本地IP访问")
		}
	}
	return nil
}
