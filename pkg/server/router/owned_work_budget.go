// 本文件在原ServiceContext内唯一登记实例工作预算，不给应用暴露另一套提交循环。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

// NewOwnedWorkBudget创建同实例所有组Runtime共用的预算，并管理指标及逆序关闭；重复声明拒绝。
func (sc *ServiceContext) NewOwnedWorkBudget(options ownership.GroupWorkBudgetOptions) (*ownership.GroupWorkBudget, error) {
	if sc == nil || sc.Service == nil {
		return nil, ErrInvalidManagedResource
	}
	sc.ownedMetricsMu.Lock()
	defer sc.ownedMetricsMu.Unlock()
	if sc.ownedWorkBudget != nil {
		return nil, ErrResourceAlreadyRegistered
	}
	sc.lifecycleMu.Lock()
	id, terminated := sc.ServiceInstanceID, sc.terminated
	sc.lifecycleMu.Unlock()
	if terminated {
		return nil, ErrResourceManagerClosed
	}
	budget, err := ownership.NewGroupWorkBudget(id, options)
	if err != nil {
		return nil, err
	}
	if err = sc.UseResource("ownership-work-budget", budget); err != nil {
		_ = budget.Close(context.Background())
		return nil, err
	}
	if err = sc.RegisterRuntimeMetricProviders(budget); err != nil {
		_ = budget.Close(context.Background())
		return nil, err
	}
	sc.ownedWorkBudget = budget
	return budget, nil
}
