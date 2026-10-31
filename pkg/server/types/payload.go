package types

import (
	"encoding/json"
)

type TargetServer struct {
	IP       string
	HttpPort int
}

type PayLoad struct {
	TraceID        string
	SourceAddress  string
	SourcePort     int
	SourceService  string
	TargetAddress  string
	TargetPort     int
	TargetService  string
	SourcePath     string
	TargetPath     string
	UserId         string
	UserName       string
	ClientIP       string
	Auth           bool
	Instance       interface{}
	Data           []byte
	HttpMethod     string
	Token          string `json:"-"`
	ServiceHashKey string `json:"-"`
	// TargetInstanceID只在调用方精确选址使用，不进入业务payload或远端协议。
	TargetInstanceID string `json:"-"`
}

func (own *PayLoad) InstanceRouter(api IRouter) error {
	values, err := json.Marshal(own.Instance)
	if err != nil {
		return err
	}
	err = json.Unmarshal(values, api)
	if err != nil {
		return err
	}
	return nil
}
