// 本文件组合稳定身份、独立编号权威和有界生成器生命周期。
package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/gofrs/uuid"
)

type stableIdentityRuntime struct {
	file      *identity.File
	authority *identity.Authority
	binding   *identity.Binding
	generator *identity.Generator
	owner     cluster.RegistrationOwner
	once      sync.Once
	closeErr  error
	stopped   atomic.Bool
	running   atomic.Bool
	attempted atomic.Bool
	mu        sync.RWMutex
	confirmed time.Time
	ttl       time.Duration
	healthy   bool
	external  bool
}

func initializeStableIdentity(sc *ServiceContext) (result *stableIdentityRuntime, err error) {
	c := sc.Config
	f, err := identity.Open(c.Cluster.IdentityFile, sc.Service.Name, c.Cluster.InstanceID)
	if err != nil {
		return nil, err
	}
	r := &stableIdentityRuntime{file: f, external: c.Cluster.Mode != "off", ttl: 10 * time.Second}
	defer func() {
		if err != nil {
			_ = r.close(true)
		}
	}()
	a, authorityName, err := identity.BuildAuthority(c.IDGenerator)
	if err != nil {
		return nil, err
	}
	r.authority = a
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = a.Check(ctx); err != nil {
		return nil, err
	}
	if f.State.Assigned && (f.State.Namespace != c.IDGenerator.Namespace || f.State.Authority != authorityName || f.State.AuthorityEpoch != c.IDGenerator.Epoch) {
		return nil, errors.New("stable identity: generator authority changed")
	}
	machine, dc := c.MachineID, c.DataCenterID
	if f.State.Assigned {
		machine = f.State.MachineID
		dc = f.State.DataCenterID
		if c.DataCenterID != dc || !c.Cluster.Claim.AutoMachineID && c.MachineID != machine {
			return nil, identity.ErrBindingConflict
		}
	}
	maxID := c.Cluster.Claim.MachineIDMax
	if maxID == 0 {
		maxID = 31
	}
	b, err := a.Bind(ctx, sc.Service.Name, f.State.InstanceID, dc, machine, maxID, c.Cluster.Claim.AutoMachineID)
	if err != nil {
		return nil, err
	}
	r.binding = b
	if f.State.BootEpoch > b.BootEpoch {
		return nil, errors.New("stable identity: authority boot epoch rolled back")
	}
	if f.State.Assigned && (f.State.WorkerID != b.WorkerID || f.State.MachineID != b.MachineID || f.State.DataCenterID != b.DataCenterID) {
		return nil, identity.ErrBindingConflict
	}
	boot := uuid.Must(uuid.NewV4()).String()
	epoch, err := a.AcquireBoot(ctx, b.Key, boot, f.State.BootID, false)
	if err != nil {
		return nil, err
	}
	r.owner = cluster.RegistrationOwner{BootID: boot, Epoch: epoch}
	f.State.GeneratorState = identity.GeneratorState{Assigned: true, Namespace: c.IDGenerator.Namespace, Authority: authorityName, AuthorityEpoch: c.IDGenerator.Epoch, Encoding: "legacy-decimal-v1", WorkerID: b.WorkerID, DataCenterID: b.DataCenterID, MachineID: b.MachineID, ReservedUntil: max(f.State.ReservedUntil, b.ReservedUntil)}
	f.State.BootID = boot
	f.State.BootEpoch = epoch
	if err = f.Save(); err != nil {
		return nil, err
	}
	wait := c.IDGenerator.StartupTimeout
	if wait == 0 {
		wait = 5 * time.Second
	}
	startCtx, startCancel := context.WithTimeout(context.Background(), wait)
	g, err := identity.NewGenerator(startCtx, a, f, b, boot, epoch, identity.GeneratorOptions{Window: c.IDGenerator.ReservationWindow})
	startCancel()
	if err != nil {
		return nil, err
	}
	r.generator = g
	g.Guard = r.generatorGuard
	c.MachineID = b.MachineID
	c.DataCenterID = b.DataCenterID
	sc.ServiceInstanceID = f.State.InstanceID
	return r, nil
}

func (r *stableIdentityRuntime) generatorGuard() error {
	if r.stopped.Load() {
		return identity.ErrOwnerConflict
	}
	if !r.running.Load() {
		return nil
	} // 仅启动期内部步骤可发 ID，外部入口另行拒绝。
	return r.admission()
}
func (r *stableIdentityRuntime) admission() error {
	if r.stopped.Load() || !r.running.Load() {
		return identity.ErrAdmissionUnavailable
	}
	if !r.external {
		return nil
	}
	r.mu.RLock()
	confirmed, ttl, healthy := r.confirmed, r.ttl, r.healthy
	r.mu.RUnlock()
	if !healthy || confirmed.IsZero() {
		return identity.ErrAdmissionUnavailable
	}
	now := time.Now()
	wall := now.UnixMilli() - confirmed.UnixMilli()
	if wall < 0 || max(now.Sub(confirmed), time.Duration(wall)*time.Millisecond) >= ttl*4/5 {
		return identity.ErrAdmissionUnavailable
	}
	return nil
}
func (r *stableIdentityRuntime) verify(ctx context.Context) error {
	if r.stopped.Load() {
		return identity.ErrOwnerConflict
	}
	return r.authority.VerifyBoot(ctx, r.binding.Key, r.owner.BootID, r.owner.Epoch)
}
func (r *stableIdentityRuntime) observe(start time.Time, err error) {
	r.mu.Lock()
	r.confirmed = start
	r.healthy = err == nil
	r.mu.Unlock()
}
func (r *stableIdentityRuntime) beginStop() {
	r.stopped.Store(true)
	if r.generator != nil {
		r.generator.Disable()
	}
}
func (r *stableIdentityRuntime) close(release bool) error {
	r.once.Do(func() {
		r.beginStop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if release && r.authority != nil && r.binding != nil && r.owner.Epoch != 0 {
			r.closeErr = r.authority.ReleaseBoot(ctx, r.binding.Key, r.owner.BootID, r.owner.Epoch)
		}
		if r.authority != nil {
			r.closeErr = errors.Join(r.closeErr, r.authority.Close())
		}
		r.closeErr = errors.Join(r.closeErr, r.file.Close())
	})
	return r.closeErr
}

// CheckServiceAdmission 返回本进程服务自身就绪状态，不检查下游依赖。
func (sc *ServiceContext) CheckServiceAdmission() error {
	if sc == nil {
		return fmt.Errorf("service unavailable")
	}
	if sc.stableIdentity == nil {
		return nil
	}
	return sc.stableIdentity.admission()
}

// CheckProcessAdmission 聚合进程自身服务，保留已清理的稳定服务终态故障。
// 同名服务成功重建后清除故障；不递归查询下游。
func CheckProcessAdmission() error { return contextRegistry.checkAdmission() }

func (r *serviceContextRegistry) checkAdmission() error {
	r.mu.RLock()
	for _, err := range r.readinessFailures {
		r.mu.RUnlock()
		return err
	}
	contexts := make([]*ServiceContext, 0, len(r.contexts))
	for _, sc := range r.contexts {
		contexts = append(contexts, sc)
	}
	r.mu.RUnlock()
	for _, sc := range contexts {
		if err := sc.CheckServiceAdmission(); err != nil {
			return err
		}
	}
	return nil
}
