// Package ownership 定义跨主题持续归属和应用事务fence，不解析业务数据。
package ownership

import (
	"errors"
	"strings"
)

var (
	ErrUnknown       = errors.New("ownership: metadata or observation unknown")
	ErrFenced        = errors.New("ownership: credential fenced")
	ErrNotReady      = errors.New("ownership: owner not ready")
	ErrBusy          = errors.New("ownership: key already owned")
	ErrStalled       = errors.New("ownership: work progress stalled")
	ErrUnsupported   = errors.New("ownership: provider capability unsupported")
	ErrStaleSnapshot = errors.New("ownership: stale reference snapshot")
	ErrCapacity      = errors.New("ownership: bounded capacity reached")
)

// Credential 是一次持续归属的身份；Token只供内部验证，不进入事件或日志。
type Credential struct {
	Scope      string
	Key        string
	Service    string
	InstanceID string
	SessionID  string
	Epoch      int64
	Token      string `json:"-"`
}

// Validate 拒绝未知epoch或不完整身份，不为恢复/回退补造0。
func (c Credential) Validate() error {
	for _, value := range []string{c.Scope, c.Key, c.Service, c.InstanceID, c.SessionID, c.Token} {
		if strings.TrimSpace(value) == "" || len(value) > 512 {
			return ErrUnknown
		}
	}
	if c.Epoch <= 0 {
		return ErrUnknown
	}
	return nil
}

// Same 比较完整凭据，不能仅比较健康实例或epoch。
func (c Credential) Same(other Credential) bool {
	return c == other && c.Validate() == nil
}

// String 的安全表示不包含Token或业务归属键。
func (c Credential) String() string { return "ownership credential (redacted)" }
