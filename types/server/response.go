package types

// IResponse
type IResponse interface {
	GetSuccess() bool                                //是否成功
	GetMessage() string                              //获取消息
	GetData(instanceType ...interface{}) interface{} //获取数据,参数为实例类型，如果为空，则返回map[string]interface{}
	GetError() error                                 //获取错误
}

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
	return own.Message
}

func (own *TypeError) GetSuggest() string {
	return ""
}
