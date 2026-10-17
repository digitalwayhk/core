// 本文件实现独立编号分配、持久启动世代与时间上界。
package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"
)

type ledger struct {
	Version   int                 `json:"version"`
	Encoding  string              `json:"encoding"`
	BaseTime  int64               `json:"base_time"`
	TimeShift int                 `json:"time_shift"`
	Epoch     string              `json:"epoch"`
	Bindings  map[string]*Binding `json:"bindings"`
	Workers   map[string]string   `json:"workers"`
}

// Authority 独立拥有编号账本，与发现 Provider 不共享客户端生命周期。
type Authority struct {
	store Store
	epoch string
}

// NewAuthority 构造显式世代的编号权威，不自动初始化远端数据。
func NewAuthority(store Store, epoch string) *Authority {
	return &Authority{store: store, epoch: epoch}
}

// Close 释放本权威持有的客户端。
func (a *Authority) Close() error { return a.store.Close() }

// Initialize 仅供显式离线初始化使用；普通运行期不得调用。
func (a *Authority) Initialize(ctx context.Context) error {
	if !segment.MatchString(a.epoch) {
		return errors.New("identity: invalid authority epoch")
	}
	raw, err := a.store.Load(ctx)
	if err == nil {
		_, err = a.decode(raw)
		return err
	}
	if !errors.Is(err, ErrAuthorityMissing) {
		return err
	}
	data, _ := json.Marshal(ledger{Version: 1, Encoding: "legacy-decimal-v1", BaseTime: GeneratorBaseTime, TimeShift: 12, Epoch: a.epoch, Bindings: map[string]*Binding{}, Workers: map[string]string{}})
	ok, err := a.store.CompareAndSwap(ctx, "", string(data))
	if err != nil {
		return err
	}
	if !ok {
		return a.Check(ctx)
	}
	return nil
}
func (a *Authority) decode(raw string) (result *ledger, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %v", ErrAuthorityInvalid, err)
		}
	}()
	var l ledger
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return nil, err
	}
	if l.Version != 1 || l.Encoding != "legacy-decimal-v1" || l.BaseTime != GeneratorBaseTime || l.TimeShift != 12 || !segment.MatchString(a.epoch) || l.Epoch != a.epoch || l.Bindings == nil || l.Workers == nil {
		return nil, errors.New("identity: authority epoch or schema mismatch")
	}
	for key, b := range l.Bindings {
		if b == nil || key != InstanceKey(b.Service, b.InstanceID) || b.Key != key || !segment.MatchString(b.Service) || !segment.MatchString(b.InstanceID) || b.ReservedUntil < 0 || (b.BootEpoch == 0 && b.BootID != "") || (b.BootEpoch > 0 && !segment.MatchString(b.BootID)) {
			return nil, errors.New("identity: corrupt binding")
		}
		worker, err := utils.StableSnowflakeWorkerID(b.MachineID, b.DataCenterID)
		if err != nil || worker != b.WorkerID {
			return nil, errors.New("identity: corrupt worker layout")
		}
		if !b.Retired && l.Workers[strconv.Itoa(int(worker))] != key {
			return nil, errors.New("identity: corrupt worker index")
		}
	}
	for worker, key := range l.Workers {
		b := l.Bindings[key]
		if b == nil || strconv.Itoa(int(b.WorkerID)) != worker {
			return nil, errors.New("identity: corrupt reverse worker index")
		}
	}
	return &l, nil
}

// Check 验证既有权威世代，丢失时拒绝恢复为空账本。
func (a *Authority) Check(ctx context.Context) error {
	raw, err := a.store.Load(ctx)
	if err != nil {
		return err
	}
	_, err = a.decode(raw)
	return err
}
func (a *Authority) update(ctx context.Context, change func(*ledger) error) error {
	for range 128 {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := a.store.Load(ctx)
		if err != nil {
			return err
		}
		l, err := a.decode(raw)
		if err != nil {
			return err
		}
		if err = change(l); err != nil {
			return err
		}
		next, err := json.Marshal(l)
		if err != nil {
			return err
		}
		if string(next) == raw {
			return nil
		}
		ok, err := a.store.CompareAndSwap(ctx, raw, string(next))
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return errors.New("identity: authority contention budget exceeded")
}

// InstanceKey 无歧义编码服务名和稳定实例 ID。
func InstanceKey(service, instance string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(service + "\x00" + instance))
}

// Bind 按实际六位 worker 原子分配，重启返回同一绑定。
func (a *Authority) Bind(ctx context.Context, service, instance string, dc, machine, max uint, auto bool) (*Binding, error) {
	if !segment.MatchString(service) || !segment.MatchString(instance) || max > 31 {
		return nil, errors.New("identity: invalid binding input")
	}
	key := InstanceKey(service, instance)
	var result Binding
	err := a.update(ctx, func(l *ledger) error {
		if b := l.Bindings[key]; b != nil {
			if b.Retired || b.DataCenterID != dc || !auto && b.MachineID != machine {
				return ErrBindingConflict
			}
			result = *b
			return nil
		}
		low, high := machine, machine
		if auto {
			low = 0
			high = max
		}
		for id := low; id <= high; id++ {
			worker, err := utils.StableSnowflakeWorkerID(id, dc)
			if err != nil {
				if !auto {
					return err
				}
				continue
			}
			wk := strconv.Itoa(int(worker))
			if holder, used := l.Workers[wk]; used {
				old := l.Bindings[holder]
				if old == nil || !old.Retired || time.Now().UnixMilli() <= old.ReservedUntil {
					continue
				}
			}
			result = Binding{Key: key, Service: service, InstanceID: instance, WorkerID: worker, DataCenterID: dc, MachineID: id, Released: true}
			copy := result
			l.Bindings[key] = &copy
			l.Workers[wk] = key
			return nil
		}
		return fmt.Errorf("%w: no valid worker available", ErrBindingConflict)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Get 返回绑定的不可变副本。
func (a *Authority) Get(ctx context.Context, key string) (*Binding, error) {
	raw, err := a.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	l, err := a.decode(raw)
	if err != nil {
		return nil, err
	}
	b := l.Bindings[key]
	if b == nil {
		return nil, ErrBindingConflict
	}
	copy := *b
	return &copy, nil
}

// AcquireBoot 仅在旧世代已释放或离线恢复已批准时接管，不把 TTL 当停机证明。
func (a *Authority) AcquireBoot(ctx context.Context, key, boot, previous string, recoveryApproved bool) (uint64, error) {
	if !segment.MatchString(boot) {
		return 0, errors.New("identity: invalid boot")
	}
	var epoch uint64
	err := a.update(ctx, func(l *ledger) error {
		b := l.Bindings[key]
		if b == nil || b.Retired {
			return ErrBindingConflict
		}
		if b.BootID == boot && !b.Released {
			epoch = b.BootEpoch
			return nil
		}
		if !b.Released && (b.BootID != previous || !recoveryApproved) {
			return ErrOwnerConflict
		}
		if b.BootEpoch == ^uint64(0) {
			return errors.New("identity: boot epoch exhausted")
		}
		b.BootEpoch++
		b.BootID = boot
		b.Released = false
		epoch = b.BootEpoch
		return nil
	})
	return epoch, err
}
func owned(l *ledger, key, boot string, epoch uint64) (*Binding, error) {
	b := l.Bindings[key]
	if b == nil || b.Retired {
		return nil, ErrBindingConflict
	}
	if b.BootID != boot || b.BootEpoch != epoch || b.Released {
		return nil, ErrOwnerConflict
	}
	return b, nil
}

// VerifyBoot 验证持久世代，不能用过期后重新创建的临时 lease 代替。
func (a *Authority) VerifyBoot(ctx context.Context, key, boot string, epoch uint64) error {
	raw, err := a.store.Load(ctx)
	if err != nil {
		return err
	}
	l, err := a.decode(raw)
	if err != nil {
		return err
	}
	_, err = owned(l, key, boot, epoch)
	return err
}

// Reserve 在任何 ID 对外可见前单调提交生成时间上界。
func (a *Authority) Reserve(ctx context.Context, key, boot string, epoch uint64, until int64) error {
	if until < 0 {
		return errors.New("identity: negative reservation")
	}
	return a.update(ctx, func(l *ledger) error {
		b, err := owned(l, key, boot, epoch)
		if err != nil {
			return err
		}
		if until > b.ReservedUntil {
			b.ReservedUntil = until
		}
		return nil
	})
}

// ReleaseBoot 只释放本世代运行所有权，不释放 worker 或删除时间上界。
func (a *Authority) ReleaseBoot(ctx context.Context, key, boot string, epoch uint64) error {
	return a.update(ctx, func(l *ledger) error {
		b, err := owned(l, key, boot, epoch)
		if err != nil {
			return err
		}
		b.Released = true
		return nil
	})
}
