package types

import (
	"encoding/json"
)

type TargetServer struct {
	IP         string
	HttpPort   int
	SocketPort int
}

type PayLoad struct {
	TraceID          string
	SourceAddress    string
	SourcePort       int
	SourceSocketPort int
	SourceService    string
	TargetAddress    string
	TargetPort       int
	TargetSocketPort int
	TargetService    string
	SourcePath       string
	TargetPath       string
	UserId           uint
	UserName         string
	ClientIP         string
	Auth             bool
	Instance         interface{}
	Data             []byte
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
