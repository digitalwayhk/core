// 本文件包装兼容 Snowflake，确保时间窗口先持久后向外返回 ID。
package identity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yitter/idgenerator-go/idgen"
)

// GeneratorBaseTime 是兼容布局的固定纪元，单位毫秒。
const GeneratorBaseTime int64 = 1582136402000

// GeneratorOptions 为测试提供时钟和源注入，生产默认使用兼容 worker。
type GeneratorOptions struct {
	Now    func() time.Time
	Source idgen.ISnowWorker
	Window time.Duration
}

// Generator 在窗口耗尽时持久预留，不在每个 ID 上执行磁盘同步。
type Generator struct {
	mu        sync.Mutex
	authority *Authority
	file      *File
	binding   *Binding
	boot      string
	epoch     uint64
	source    idgen.ISnowWorker
	now       func() time.Time
	window    int64
	until     int64
	floor     int64
	lastWall  int64
	closed    bool
	// Guard 必须在对外发布 worker 前设置，用于本地租约准入。
	Guard func() error
}

// NewGenerator 等待时钟越过历史最大预留范围后创建本次启动的 worker。
func NewGenerator(ctx context.Context, a *Authority, f *File, b *Binding, boot string, epoch uint64, o GeneratorOptions) (*Generator, error) {
	if a == nil || f == nil || b == nil || b.WorkerID > 63 {
		return nil, errors.New("identity: invalid worker layout")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Window == 0 {
		o.Window = time.Second
	}
	if o.Window < time.Millisecond || o.Window > time.Minute {
		return nil, errors.New("identity: reservation window too large")
	}
	current, err := a.Get(ctx, b.Key)
	if err != nil {
		return nil, err
	}
	if current.BootID != boot || current.BootEpoch != epoch || current.Released || current.Retired {
		return nil, ErrOwnerConflict
	}
	floor := max(current.ReservedUntil, f.State.ReservedUntil)
	for o.Now().UnixMilli() <= floor {
		if _, bounded := ctx.Deadline(); !bounded {
			return nil, errors.New("identity: clock must advance past reserved time")
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if o.Source == nil {
		o.Source = idgen.NewSnowWorkerM1(idgen.NewIdGeneratorOptions(b.WorkerID))
	}
	g := &Generator{authority: a, file: f, binding: b, boot: boot, epoch: epoch, source: o.Source, now: o.Now, window: o.Window.Milliseconds(), floor: floor, lastWall: o.Now().UnixMilli()}
	if err = g.reserve(ctx, g.lastWall); err != nil {
		return nil, err
	}
	return g, nil
}
func (g *Generator) reserve(ctx context.Context, tick int64) error {
	until := tick + g.window
	if until < tick {
		return errors.New("identity: reservation overflow")
	}
	if err := g.authority.Reserve(ctx, g.binding.Key, g.boot, g.epoch, until); err != nil {
		return err
	}
	g.file.State.ReservedUntil = until
	if err := g.file.Save(); err != nil {
		return err
	}
	g.until = until
	return nil
}

// NextId 拒绝无保护 ID；失败以可识别 panic 保持旧公开签名，不返回零或伪成功。
func (g *Generator) NextId() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	fail := func(err error) {
		if !errors.Is(err, ErrAdmissionUnavailable) {
			g.closed = true
		}
		panic(fmt.Errorf("stable generator unavailable: %w", err))
	}
	if g.closed {
		fail(errors.New("generator is closed"))
	}
	if g.Guard != nil {
		if err := g.Guard(); err != nil {
			fail(err)
		}
	}
	wall := g.now().UnixMilli()
	if wall < g.lastWall {
		fail(errors.New("clock moved backwards"))
	}
	g.lastWall = wall
	id := g.source.NextId()
	if uint16((id>>6)&63) != g.binding.WorkerID {
		fail(errors.New("generated worker does not match binding"))
	}
	if id < 0 {
		fail(errors.New("negative generated ID"))
	}
	tick := (id >> 12) + GeneratorBaseTime
	if tick <= g.floor {
		fail(errors.New("generated time overlaps previous boot"))
	}
	if tick > g.until {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := g.reserve(ctx, max(tick, wall))
		cancel()
		if err != nil {
			fail(err)
		}
	}
	if g.Guard != nil {
		if err := g.Guard(); err != nil {
			fail(err)
		}
	}
	return id
}

// Close 停止生成并释放本次启动的编号世代，不释放编号本身。
func (g *Generator) Close(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return g.authority.ReleaseBoot(ctx, g.binding.Key, g.boot, g.epoch)
}

// Disable 同步阻止新 ID，尚不释放持久 boot，供优雅退出顺序控制。
func (g *Generator) Disable() { g.mu.Lock(); g.closed = true; g.mu.Unlock() }
