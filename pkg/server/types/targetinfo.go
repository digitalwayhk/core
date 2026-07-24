package types

// TargetInfo 描述一次显式跨服务调用的目标。
type TargetInfo struct {
	TargetAddress  string
	TargetService  string
	TargetPort     int
	TargetGRPCPort int
	TargetPath     string
	TargetToken    string
}
