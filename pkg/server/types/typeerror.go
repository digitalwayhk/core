package types

import "fmt"

type TypeError struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	ServiceName string `json:"-"` //服务名
	Path        string `json:"-"` // 路径
	Type        string `json:"-"` //操作类型
	Suggest     string `json:"suggest"`
}

func NewTypeError(serviceName, path, ot, mes string, code int) *TypeError {
	return &TypeError{
		Code:        code,
		Message:     mes,
		ServiceName: serviceName,
		Path:        path,
		Type:        ot,
		Suggest:     "",
	}
}
func (own *TypeError) Error() string {
	return fmt.Sprintf("[%s] %s %s (code=%d): %s", own.ServiceName, own.Type, own.Path, own.Code, own.Message)
}

func (own *TypeError) GetSuggest() string {
	return ""
}
