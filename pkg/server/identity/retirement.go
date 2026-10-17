// 本文件提供显式离线退役与时间安全的编号复用，不删除业务事实。
package identity

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/utils"
	"strconv"
	"strings"
	"time"
)

// RetirementProof 是运维人工核对后提供的停机、数据处理及绑定版本清单。
// Evidence 是受信操作员的声明，工具不能代替编排层完成物理隔离。
type RetirementProof struct {
	Key             string `json:"key"`
	Epoch           string `json:"epoch"`
	BootEpoch       uint64 `json:"boot_epoch"`
	ReservedUntil   int64  `json:"reserved_until"`
	StoppedEvidence string `json:"stopped_evidence"`
	PendingEvidence string `json:"pending_evidence"`
}

// Retire 在同一 CAS 内核对所有条件，dryRun 永不写账本。
func (a *Authority) Retire(ctx context.Context, p RetirementProof, dryRun bool) error {
	if p.Epoch != a.epoch || strings.TrimSpace(p.StoppedEvidence) == "" || strings.TrimSpace(p.PendingEvidence) == "" {
		return errors.New("identity: retirement epoch and external evidence required")
	}
	check := func(l *ledger) error {
		b := l.Bindings[p.Key]
		if b == nil || b.Retired {
			return ErrBindingConflict
		}
		if !b.Released || b.BootEpoch != p.BootEpoch || b.ReservedUntil != p.ReservedUntil {
			return errors.New("identity: active or changed binding cannot retire")
		}
		if time.Now().UnixMilli() <= b.ReservedUntil {
			return errors.New("identity: reserved time quarantine is not over")
		}
		if !dryRun {
			b.Retired = true
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

// ImportReservation 只为已释放的绑定导入可信历史时间上界。
func (a *Authority) ImportReservation(ctx context.Context, key string, until int64) error {
	if until < 0 {
		return errors.New("identity: invalid imported time")
	}
	return a.update(ctx, func(l *ledger) error {
		b := l.Bindings[key]
		if b == nil || b.Retired {
			return ErrBindingConflict
		}
		if !b.Released {
			return ErrOwnerConflict
		}
		b.ReservedUntil = max(b.ReservedUntil, until)
		return nil
	})
}

// Capacity 报告当前 DC 下真正有效的兼容 worker 空间。
func (a *Authority) Capacity(ctx context.Context, dc, maxMachine uint) (map[string]int, error) {
	if maxMachine > 31 {
		return nil, errors.New("identity: invalid machine range")
	}
	raw, err := a.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	l, err := a.decode(raw)
	if err != nil {
		return nil, err
	}
	result := map[string]int{"total": 0, "bound": 0, "quarantined": 0, "available": 0}
	seen := map[uint16]bool{}
	for machine := uint(0); machine <= maxMachine; machine++ {
		worker, err := utils.StableSnowflakeWorkerID(machine, dc)
		if err != nil || seen[worker] {
			continue
		}
		seen[worker] = true
		result["total"]++
		holder := l.Workers[strconv.Itoa(int(worker))]
		b := l.Bindings[holder]
		if holder == "" {
			result["available"]++
		} else if b != nil && b.Retired && time.Now().UnixMilli() > b.ReservedUntil {
			result["available"]++
		} else if b != nil && b.Retired {
			result["quarantined"]++
		} else {
			result["bound"]++
		}
	}
	return result, nil
}
