// 本文件验证轻量排空探测不能把缺失、负数或消费积压视为完成。
package shoporderscalemultiprocess

import (
	"testing"
	"time"
)

// TestLoadConvergenceRejectsUnknownState 验证只有全部已观测状态收敛才允许记录排空时间。
func TestLoadConvergenceRejectsUnknownState(t *testing.T) {
	zero := float64(0)
	negative := float64(-1)
	no := false
	yes := true
	count := int64(0)
	one := int64(1)
	good := orderLoadSample{Pending: &zero, Unpublished: &no, BrokerPending: &count, BrokerLag: &count}
	if !good.converged() {
		t.Fatal("完整收敛应通过")
	}
	for _, change := range []func(*orderLoadSample){
		func(s *orderLoadSample) { s.Pending = nil }, func(s *orderLoadSample) { s.Pending = &negative },
		func(s *orderLoadSample) { s.Unpublished = nil }, func(s *orderLoadSample) { s.Unpublished = &yes },
		func(s *orderLoadSample) { s.BrokerLag = &one }, func(s *orderLoadSample) { s.BrokerPending = nil },
		func(s *orderLoadSample) { s.Errors = []string{"缺少指标"} },
	} {
		sample := good
		change(&sample)
		if sample.converged() {
			t.Fatal("异常/未知状态被误判收敛")
		}
	}
}

// TestObservedDrainUsesProbeCompletion 验证结果文件晚到不改变已完成观测的时间。
func TestObservedDrainUsesProbeCompletion(t *testing.T) {
	end := time.Now().Add(-10 * time.Second)
	zero := float64(0)
	no := false
	count := int64(0)
	good := func(at time.Time) orderLoadSample {
		return orderLoadSample{At: at, Pending: &zero, Unpublished: &no, BrokerPending: &count, BrokerLag: &count}
	}
	store, drain := observedOrderDrain(end, []orderLoadSample{good(end.Add(-time.Second)), good(end.Add(time.Second)), good(end.Add(2 * time.Second))})
	if store != 1 || drain != 2 {
		t.Fatalf("应使用观测完成时刻，得到store=%v drain=%v", store, drain)
	}
}

// TestObservedDrainRequiresTwoCompleteSamples 验证负数、缺失、错误和消费积压都会打断连续收敛。
func TestObservedDrainRequiresTwoCompleteSamples(t *testing.T) {
	end := time.Now().Add(-10 * time.Second)
	zero := float64(0)
	negative := float64(-1)
	no := false
	count := int64(0)
	one := int64(1)
	good := func(n int) orderLoadSample {
		return orderLoadSample{At: end.Add(time.Duration(n) * time.Second), Pending: &zero, Unpublished: &no, BrokerPending: &count, BrokerLag: &count}
	}
	for _, change := range []func(*orderLoadSample){func(s *orderLoadSample) { s.Pending = &negative }, func(s *orderLoadSample) { s.Pending = nil }, func(s *orderLoadSample) { s.Errors = []string{"观测失败"} }, func(s *orderLoadSample) { s.BrokerLag = &one }} {
		bad := good(2)
		change(&bad)
		_, drain := observedOrderDrain(end, []orderLoadSample{good(1), bad, good(3)})
		if drain >= 0 {
			t.Fatalf("不能跨异常观测拼接连续收敛：%v", drain)
		}
		_, drain = observedOrderDrain(end, []orderLoadSample{good(1), bad, good(3), good(4)})
		if drain != 4 {
			t.Fatalf("应在两次完整新观测后收敛：%v", drain)
		}
	}
}
