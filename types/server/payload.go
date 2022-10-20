package types

import (
	"encoding/json"
	"errors"
)

// ServerInfo 服务器信息
type ServerInfo struct {
	Address    string //服务器地址
	Service    string //服务器服务名称
	Port       int    //服务器端口
	SocketPort int    //服务器socket协议端口
	Path       string //服务器路由路径
}

// PayLoad 转发数据载体
type PayLoad struct {
	TraceID      string
	SourceServer *ServerInfo
	TargetServer *ServerInfo
	UserId       uint
	UserName     string
	ClientIP     string
	Auth         bool
	Instance     interface{}
	Data         []byte
}

func (own *PayLoad) InstanceRouter(api IRouter) (IRouter, error) {
	if api == nil {
		return nil, errors.New("api is nil")
	}
	values, err := json.Marshal(own.Instance)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(values, api)
	if err != nil {
		return nil, err
	}
	return api, nil
}
