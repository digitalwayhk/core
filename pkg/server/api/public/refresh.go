package public

import (
	"errors"
	"net/http"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Refresh 使用内置 Refresh Token 换取新的 Access Token，不旋转 Refresh Token。
type Refresh struct {
	Token string `json:"token"`
	Type  string `json:"type" desc:"auth 或 manage，默认 auth"`
}

func (own *Refresh) Parse(req types.IRequest) error {
	return req.Bind(own)
}

func (own *Refresh) Validation(types.IRequest) error {
	if own.Type == "" {
		own.Type = string(types.AuthTypeUser)
	}
	if own.Token == "" {
		return errors.New("Refresh Token 不能为空")
	}
	if own.Type != string(types.AuthTypeUser) && own.Type != string(types.AuthTypeManage) {
		return errors.New("刷新认证类型无效")
	}
	return nil
}

func (own *Refresh) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	return refreshForServiceAt(requestContext(req), sc, own.Token, types.AuthType(own.Type), time.Now().UTC())
}

func (*Refresh) GetResponse() interface{} {
	return &safe.TokenPairResponse{}
}

func (own *Refresh) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own,
		router.WithMethod(http.MethodPost),
		router.WithPath("/api/refresh"),
		withAuthEndpointRateLimit(),
	)
}
