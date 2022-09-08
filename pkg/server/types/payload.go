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

// func (own *PayLoad) Send(ts *TargetServer) ([]byte, error) {
// 	values, err := json.Marshal(own)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if ts.SocketPort > 0 {
// 		client := socket.Client{
// 			Server: socket.Server{
// 				IP:   ts.IP,
// 				Port: ts.SocketPort,
// 			},
// 		}
// 		err := client.Connect()
// 		if err != nil {
// 			return nil, err
// 		}
// 		txt, err := client.Send(values)
// 		if err != nil {
// 			return nil, err
// 		}
// 		client.Close()
// 		return txt, err
// 	}
// 	path := ts.IP + ":" + fmt.Sprintf("%d", ts.HttpPort) + own.TargetPath
// 	return rest.PostJson(path, values)
// }

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
