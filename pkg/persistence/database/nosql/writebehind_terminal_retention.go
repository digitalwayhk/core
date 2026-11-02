// 本文件定义额外终态审计的有限预算；未解决原件不受此配置淘汰。
package nosql

import "time"

// WriteBehindTerminalRetention 仅控制completed/released/superseded的额外完整审计快照。
// nil默认立即瘦身；显式启用要求时限、条数及编码字节三种预算均为正，超限只放弃额外载荷。
type WriteBehindTerminalRetention struct {
	MaxAge     time.Duration `json:"max_age" yaml:"max_age"`
	MaxRecords int64         `json:"max_records" yaml:"max_records"`
	MaxBytes   int64         `json:"max_bytes" yaml:"max_bytes"`
}

// Validate 拒绝无限保留、空配置及负预算。
func (r WriteBehindTerminalRetention) Validate() error {
	if r.MaxAge <= 0 || r.MaxRecords <= 0 || r.MaxBytes <= 0 {
		return ErrWriteBehindDisposition
	}
	return nil
}
