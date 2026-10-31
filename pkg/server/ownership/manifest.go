// 本文件冻结归属范围、原消费组和资源预算，避免不同实例解释同一组时漂移。
package ownership

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Manifest 是显式维护初始化的范围声明；Keys必须覆盖该范围全部稳定归属键。
type Manifest struct {
	Scope           string
	Service         string
	Group           string
	Selector        string
	Subjects        []string
	Keys            []string
	Window          int64
	MaxBatch        int
	MaxReferences   int
	MaxMessageBytes int64
	Timing          Timing
}

// Normalize 返回独立且排序后的声明；旧共享组迁移默认W=8，不改变原组游标。
func (m Manifest) Normalize() Manifest {
	m.Subjects = append([]string(nil), m.Subjects...)
	m.Keys = append([]string(nil), m.Keys...)
	sort.Strings(m.Subjects)
	sort.Strings(m.Keys)
	if m.Window == 0 {
		m.Window = 8
	}
	if m.MaxBatch == 0 {
		m.MaxBatch = 8
	}
	if m.MaxReferences == 0 {
		m.MaxReferences = 1024
	}
	if m.MaxMessageBytes == 0 {
		m.MaxMessageBytes = 1 << 20
	}
	m.Timing = m.Timing.Normalize()
	return m
}

// Validate 拒绝无限范围、重复键和不完整声明。
func (m Manifest) Validate() error {
	for _, v := range []string{m.Scope, m.Service, m.Group, m.Selector} {
		if strings.TrimSpace(v) == "" || len(v) > 256 {
			return ErrUnknown
		}
	}
	if len(m.Subjects) == 0 || len(m.Subjects) > 16 || len(m.Keys) == 0 || len(m.Keys) > 256 || m.Window <= 0 || m.Window > 1024 || m.MaxBatch <= 0 || m.MaxBatch > 1024 || m.MaxReferences <= 0 || m.MaxReferences > 4096 || m.MaxMessageBytes <= 0 || m.MaxMessageBytes > 16<<20 {
		return ErrCapacity
	}
	for _, list := range [][]string{m.Subjects, m.Keys} {
		seen := map[string]bool{}
		for _, v := range list {
			if strings.TrimSpace(v) == "" || len(v) > 512 || seen[v] {
				return ErrUnknown
			}
			seen[v] = true
		}
	}
	return m.Timing.Validate()
}

// Fingerprint 标识冻结合同，不包含实例身份或业务payload。
func (m Manifest) Fingerprint() string {
	data, _ := json.Marshal(m.Normalize())
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
