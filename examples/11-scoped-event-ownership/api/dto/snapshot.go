// Package dto 暴露不含租约token或数据库信息的演示快照。
package dto

// Snapshot 是已提交计数与当前归属状态；Key是演示用户，不能作为指标标签。
type Snapshot struct {
	Key               string `json:"key"`
	Count             int64  `json:"count"`
	Epoch             int64  `json:"epoch"`
	InstanceID        string `json:"instanceID"`
	PID               int    `json:"pid"`
	References        int    `json:"references"`
	ReferenceRevision int64  `json:"referenceRevision"`
}
