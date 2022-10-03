package public

import (
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type TestToken struct {
	api.ServerArgs
	UserID    uint
	TokenType int //0:普通用户token,1:管理员token,2:管理服务token
}

func (own *TestToken) Parse(req types.IRequest) error {
	id := req.GetValue("userid")
	if id == "" {
		return errors.New("userid is empty")
	}
	userid, err := strconv.Atoi(id)
	if err != nil {
		return err
	}
	own.UserID = uint(userid)
	t := req.GetValue("type")
	if t != "" {
		ti, err := strconv.Atoi(t)
		if err != nil {
			return err
		}
		own.TokenType = ti
	}
	return nil
}
func (own *TestToken) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	con := sc.Config
	token, err := safe.GetToken(own.UserID, con.Auth.AccessSecret, con.Auth.AccessExpire)
	if own.TokenType == 1 {
		token, err = safe.GetToken(own.UserID, con.ManageAuth.AccessSecret, con.ManageAuth.AccessExpire)
	}
	if own.TokenType == 2 {
		token, err = safe.GetToken(own.UserID, con.ServerManageAuth.AccessSecret, con.ServerManageAuth.AccessExpire)
	}
	return token, err
}

func (own *TestToken) RouterInfo() *types.RouterInfo {
	info := api.ServerRouterInfo(own)
	info.Method = "GET"
	return info
}
