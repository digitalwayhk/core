package stats

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// EngineName 统计执行后端。
type EngineName string

const (
	// EngineOLTP 默认：扫 OLTP 聚合（MySQL/SQLite）。
	EngineOLTP EngineName = "oltp"
	// EngineClickHouse 读 ClickHouse 物化视图 / 聚合表。
	EngineClickHouse EngineName = "clickhouse"
)

// StatsEngine 统计刷新后端：同一 StatSpec，可换实现。
// API/Store 不感知引擎；仅 Runner 选择引擎。
type StatsEngine interface {
	// Name 引擎标识：oltp | clickhouse
	Name() EngineName
	// Ensure 确保后端就绪（OLTP 为空操作；CH 建/更新 MV 配置）。
	Ensure(ctx context.Context, spec StatSpec) error
	// Refresh 按 Spec 与时间窗聚合，返回行（不写 Store）。
	Refresh(ctx context.Context, spec StatSpec, opt ExecOptions) ([]StatRow, error)
}

// EngineConfig 进程级引擎选择。
type EngineConfig struct {
	// Name 默认 oltp；可用环境变量 CORE_STATS_ENGINE 覆盖。
	Name EngineName
	// AutoEnsure 启动/刷新前对 Spec 调用 Ensure（CH 建 MV）。
	AutoEnsure bool
	// FallbackOLTP CH 刷新失败时是否回退 OLTP（默认 true）。
	FallbackOLTP bool
}

// DefaultEngineConfig 读取默认配置。
func DefaultEngineConfig() EngineConfig {
	name := EngineOLTP
	if v := strings.TrimSpace(os.Getenv("CORE_STATS_ENGINE")); v != "" {
		name = EngineName(strings.ToLower(v))
	}
	fallback := true
	if v := strings.TrimSpace(os.Getenv("CORE_STATS_FALLBACK_OLTP")); v == "0" || strings.EqualFold(v, "false") {
		fallback = false
	}
	autoEnsure := true
	if v := strings.TrimSpace(os.Getenv("CORE_STATS_AUTO_ENSURE")); v == "0" || strings.EqualFold(v, "false") {
		autoEnsure = false
	}
	return EngineConfig{
		Name:         name,
		AutoEnsure:   autoEnsure,
		FallbackOLTP: fallback,
	}
}

var (
	engineMu       sync.RWMutex
	globalEngine   StatsEngine
	globalFallback StatsEngine
	globalCfg      = DefaultEngineConfig()
)

// SetEngineConfig 设置全局引擎配置（服务启动时调用）。
func SetEngineConfig(cfg EngineConfig) {
	engineMu.Lock()
	defer engineMu.Unlock()
	if cfg.Name == "" {
		cfg.Name = EngineOLTP
	}
	globalCfg = cfg
}

// EngineConfigSnapshot 返回当前配置副本。
func EngineConfigSnapshot() EngineConfig {
	engineMu.RLock()
	defer engineMu.RUnlock()
	return globalCfg
}

// SetEngine 注册主引擎（及可选 OLTP 回退引擎）。
func SetEngine(primary StatsEngine, oltpFallback StatsEngine) {
	engineMu.Lock()
	defer engineMu.Unlock()
	globalEngine = primary
	if oltpFallback != nil {
		globalFallback = oltpFallback
	}
}

// CurrentEngine 返回当前主引擎；未设置时默认空 OLTPEngine（需 Action/ActionFn）。
func CurrentEngine() StatsEngine {
	engineMu.RLock()
	defer engineMu.RUnlock()
	if globalEngine != nil {
		return globalEngine
	}
	if globalFallback != nil {
		return globalFallback
	}
	return &OLTPEngine{}
}

// ResolveEngine 按配置名解析引擎。
func ResolveEngine(name EngineName) (StatsEngine, error) {
	engineMu.RLock()
	defer engineMu.RUnlock()
	switch name {
	case "", EngineOLTP:
		if globalFallback != nil {
			return globalFallback, nil
		}
		if globalEngine != nil && globalEngine.Name() == EngineOLTP {
			return globalEngine, nil
		}
		return &OLTPEngine{}, nil
	case EngineClickHouse:
		if globalEngine != nil && globalEngine.Name() == EngineClickHouse {
			return globalEngine, nil
		}
		return nil, fmt.Errorf("clickhouse 引擎未注入：请 SetEngine(NewClickHouseEngine(...), oltpFallback)")
	default:
		return nil, fmt.Errorf("未知统计引擎: %s", name)
	}
}

// RefreshWithEngine 使用指定引擎刷新并写入 Store；可按配置回退 OLTP。
func RefreshWithEngine(ctx context.Context, store *Store, engine StatsEngine, spec StatSpec, opt ExecOptions) (Snapshot, error) {
	if store == nil {
		store = DefaultStore
	}
	spec = normalizeSpec(spec)
	cfg := EngineConfigSnapshot()

	if engine == nil {
		engine = CurrentEngine()
	}
	if cfg.AutoEnsure {
		if err := engine.Ensure(ctx, spec); err != nil && engine.Name() != EngineOLTP {
			if !cfg.FallbackOLTP {
				return putFailSnap(store, spec, err)
			}
		}
	}

	rows, err := engine.Refresh(ctx, spec, opt)
	if err != nil && cfg.FallbackOLTP && engine.Name() != EngineOLTP {
		engineMu.RLock()
		fb := globalFallback
		engineMu.RUnlock()
		if fb == nil {
			fb = &OLTPEngine{}
		}
		if rows2, err2 := fb.Refresh(ctx, spec, opt); err2 == nil {
			rows, err = rows2, nil
		}
	}

	snap := Snapshot{
		Code:       spec.Code,
		Title:      spec.Title,
		Grain:      spec.Grain,
		ComputedAt: time.Now().UTC(),
		Rows:       rows,
	}
	if err != nil {
		snap.Error = err.Error()
		if old, ok := store.Get(spec.Code); ok && old.Error == "" {
			return old, err
		}
		store.Put(snap)
		return snap, err
	}
	store.Put(snap)
	return snap, nil
}

func putFailSnap(store *Store, spec StatSpec, err error) (Snapshot, error) {
	snap := Snapshot{
		Code:       spec.Code,
		Title:      spec.Title,
		Grain:      spec.Grain,
		ComputedAt: time.Now().UTC(),
		Error:      err.Error(),
	}
	if old, ok := store.Get(spec.Code); ok && old.Error == "" {
		return old, err
	}
	store.Put(snap)
	return snap, err
}
