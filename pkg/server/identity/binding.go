// 本文件定义编号权威中的持久绑定和启动世代。
package identity

// Binding 保存最终 worker 及其唯一持有者；没有发现 TTL。
type Binding struct {
	Key           string `json:"key"`
	Service       string `json:"service"`
	InstanceID    string `json:"instance_id"`
	WorkerID      uint16 `json:"worker_id"`
	DataCenterID  uint   `json:"datacenter_id"`
	MachineID     uint   `json:"machine_id"`
	BootID        string `json:"boot_id"`
	BootEpoch     uint64 `json:"boot_epoch"`
	Released      bool   `json:"released"`
	Retired       bool   `json:"retired"`
	ReservedUntil int64  `json:"reserved_until"`
}
