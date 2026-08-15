package stats

import (
	"context"
	"fmt"

	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// OLTPEngine 使用 IDataAction + GORM 扫业务库聚合（默认引擎）。
type OLTPEngine struct {
	// Action 固定数据访问器；与 ActionFn 二选一。
	Action types.IDataAction
	// ActionFn 每次刷新时获取访问器（推荐：远程权威库懒连接）。
	ActionFn func() types.IDataAction
}

// NewOLTPEngine 创建 OLTP 引擎。
func NewOLTPEngine(action types.IDataAction) *OLTPEngine {
	return &OLTPEngine{Action: action}
}

// NewOLTPEngineFunc 用工厂函数创建引擎。
func NewOLTPEngineFunc(fn func() types.IDataAction) *OLTPEngine {
	return &OLTPEngine{ActionFn: fn}
}

// Name 实现 StatsEngine。
func (e *OLTPEngine) Name() EngineName { return EngineOLTP }

// Ensure OLTP 无需预建对象。
func (e *OLTPEngine) Ensure(context.Context, StatSpec) error { return nil }

// Refresh 执行 OLTP 聚合。
func (e *OLTPEngine) Refresh(ctx context.Context, spec StatSpec, opt ExecOptions) ([]StatRow, error) {
	action := e.resolveAction()
	if action == nil {
		return nil, fmt.Errorf("OLTP 引擎未配置 IDataAction")
	}
	return Exec(ctx, action, spec, opt)
}

func (e *OLTPEngine) resolveAction() types.IDataAction {
	if e == nil {
		return nil
	}
	if e.ActionFn != nil {
		return e.ActionFn()
	}
	return e.Action
}
