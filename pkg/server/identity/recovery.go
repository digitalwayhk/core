// 本文件提供外部隔离后显式批准崩溃实例重新启动的离线入口。
package identity

import (
	"context"
	"errors"
	"strings"
)

// RecoveryProof 必须由可信操作员核对；本工具不执行主机或存储隔离。
type RecoveryProof struct {
	Key             string `json:"key"`
	Epoch           string `json:"epoch"`
	BootID          string `json:"boot_id"`
	BootEpoch       uint64 `json:"boot_epoch"`
	ReservedUntil   int64  `json:"reserved_until"`
	StoppedEvidence string `json:"stopped_evidence"`
	PendingEvidence string `json:"pending_evidence"`
}

// RecoverStoppedBoot 在保留编号和时间上界的前提下释放经过隔离的旧世代。
func (a *Authority) RecoverStoppedBoot(ctx context.Context, p RecoveryProof, dryRun bool) error {
	if p.Epoch != a.epoch || strings.TrimSpace(p.StoppedEvidence) == "" || strings.TrimSpace(p.PendingEvidence) == "" {
		return errors.New("identity: recovery requires matching epoch and external isolation evidence")
	}
	check := func(l *ledger) error {
		b := l.Bindings[p.Key]
		if b == nil || b.Retired {
			return ErrBindingConflict
		}
		if b.BootID != p.BootID || b.BootEpoch != p.BootEpoch || b.ReservedUntil != p.ReservedUntil {
			return ErrOwnerConflict
		}
		if !dryRun {
			b.Released = true
		}
		return nil
	}
	if dryRun {
		raw, err := a.store.Load(ctx)
		if err != nil {
			return err
		}
		l, err := a.decode(raw)
		if err != nil {
			return err
		}
		return check(l)
	}
	return a.update(ctx, check)
}
