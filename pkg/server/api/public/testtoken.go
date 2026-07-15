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
	if own.TokenType < 0 || own.TokenType > 2 {
		return errors.New("token type is invalid")
	}
	return nil
}
func (own *TestToken) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	authType := types.AuthTypeUser
	switch own.TokenType {
	case 1:
		authType = types.AuthTypeManage
	case 2:
		authType = types.AuthTypeServerManage
	}
	return issueForService(requestContext(req), sc, own.UserID, "", authType, types.AuthSourceTestToken, nil)
}

func (*TestToken) GetResponse() interface{} {
	return &safe.TokenPairResponse{}
}

func (own *TestToken) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own, router.WithMethod("GET"))
}
