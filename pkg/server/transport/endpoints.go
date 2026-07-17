package transport

// TransportEndpoints 保存一次服务解析得到的协议专属端点。
// 不同协议不能共用 host:port，否则健康检查可能探测错误的监听端口。
type TransportEndpoints struct {
	GRPC string
	HTTP string
}

// For 返回指定协议的端点；未知协议或未发布的端点返回空字符串。
func (e TransportEndpoints) For(protocol string) string {
	switch protocol {
	case "grpc":
		return e.GRPC
	case "http":
		return e.HTTP
	default:
		return ""
	}
}

// Selection 是发送前已经完成协议能力和健康检查的不可变选择结果。
type Selection struct {
	Transport Transport
	Endpoint  string
}
