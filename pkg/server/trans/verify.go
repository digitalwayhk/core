package trans

import (
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/utils"
)

func VerifyIPWhiteList(con *config.ServerConfig, ip string) error {
	if con.IsLoaclVisit {
		if !utils.HasLocalIPAddr(ip) {
			return errors.New("只能本地IP访问")
		}
	}
	if con.IsWhiteList {
		okip := false
		for _, ipw := range con.WhiteList {
			if ipw == ip {
				okip = true
				break
			}
		}
		if !okip {
			list := entity.NewModelList[smodels.IPWhiteModel](nil)
			rows, _ := list.SearchName(ip)
			for _, r := range rows {
				if !time.Now().After(r.CreatedAt.Add(time.Duration(r.Timeout) * time.Minute)) {
					return nil
				} else {
					return errors.New("IP地址白名单已经超时")
				}
			}
			return errors.New("IP地址不在白名单中")
		}
	}
	return nil
}
