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
	UserID    string
	TokenType int //0:普通用户token,1:管理员token,2:管理服务token
}

func (own *TestToken) Parse(req types.IRequest) error {
	id := req.GetValue("userid")
	if id == "" {
		return errors.New("userid is empty")
	}
	own.UserID = id
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
	jwt := safe.NewClaims(own.UserID, "")
	token, err := jwt.GetToken(con.Auth.AccessSecret, con.Auth.AccessExpire)
	if own.TokenType == 1 {
		token, err = jwt.GetToken(con.ManageAuth.AccessSecret, con.ManageAuth.AccessExpire)
	}
	if own.TokenType == 2 {
		token, err = jwt.GetToken(con.ServerManageAuth.AccessSecret, con.ServerManageAuth.AccessExpire)
	}
	return token, err
}

func (own *TestToken) RouterInfo() *types.RouterInfo {
	info := api.ServerRouterInfo(own)
	info.Method = "GET"
	return info
}
