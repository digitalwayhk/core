// 本文件验证轻量排空探测不能把缺失、负数或消费积压视为完成。
package shoporderscalemultiprocess

import "testing"

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
