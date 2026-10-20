package multiinstance_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

type receipt struct {
	EventID     string    `json:"eventID"`
	Sequence    int       `json:"sequence"`
	State       string    `json:"state"`
	Attempts    int       `json:"attempts"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}
type progress struct {
	PID        int       `json:"pid"`
	InstanceID string    `json:"instanceID"`
	Receipts   []receipt `json:"receipts"`
}

func TestReliableMultiInstanceHTTPFlow(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	prefix := fmt.Sprintf("core:example10:%d", time.Now().UnixNano())
	start := func(role string, index int) *integration.Suite {
		s, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/10-reliable-multi-instance/main", BinaryName: fmt.Sprintf("demo10-%s-%d", role, index), TempPrefix: "core-demo10-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: map[string]string{"CORE_DEMO10_ROLE": role, "CORE_DEMO10_INDEX": strconv.Itoa(index), "CORE_DEMO10_REDIS_ADDR": addr, "CORE_DEMO10_PREFIX": prefix}})
		require.NoError(t, err)
		t.Cleanup(s.Stop)
		return s
	}
	producer, a, b := start("producer", 3), start("worker", 1), start("worker", 2)
	users := []string{"101", "202"}
	tokens := map[*integration.Suite]map[string]string{}
	for _, s := range []*integration.Suite{producer, a, b} {
		tokens[s] = map[string]string{}
		for _, uid := range users {
			tokens[s][uid] = s.TokenFor(t, uid, 0)
		}
	}
	unauth := producer.RequestJSON(t, http.MethodPost, "/api/event-submit/submit", "", map[string]interface{}{"requestID": "anonymous", "sequence": 1})
	require.False(t, unauth.Success)
	read := func(s *integration.Suite, uid string) progress {
		result := s.RequestJSON(t, http.MethodPost, "/api/event-worker/progress", tokens[s][uid], nil)
		require.True(t, result.Success, result.ErrorMessage)
		var p progress
		require.NoError(t, json.Unmarshal(result.Data, &p))
		return p
	}
	submit := func(uid, id string, sequence, delay int, fail bool) string {
		result := producer.RequestJSON(t, http.MethodPost, "/api/event-submit/submit", tokens[producer][uid], map[string]interface{}{"requestID": id, "sequence": sequence, "delayMillis": delay, "failFirst": fail, "userID": "forged-user"})
		require.True(t, result.Success, result.ErrorMessage)
		var response struct {
			EventID string `json:"eventID"`
		}
		require.NoError(t, json.Unmarshal(result.Data, &response))
		return response.EventID
	}
	find := func(uid, id, state string) (progress, receipt, bool) {
		for _, s := range []*integration.Suite{a, b} {
			p := read(s, uid)
			for _, item := range p.Receipts {
				if item.EventID == id && item.State == state {
					return p, item, true
				}
			}
		}
		return progress{}, receipt{}, false
	}
	slow := submit("101", "slow-1", 1, 3000, false)
	var slowProgress progress
	require.Eventually(t, func() bool { p, _, ok := find("101", slow, "running"); slowProgress = p; return ok }, 2*time.Second, 10*time.Millisecond)
	independent := submit("202", "independent-1", 1, 0, false)
	var fastProgress progress
	require.Eventually(t, func() bool { p, _, ok := find("202", independent, "completed"); fastProgress = p; return ok }, 2*time.Second, 10*time.Millisecond)
	require.NotEqual(t, slowProgress.PID, fastProgress.PID, "两个真实服务副本必须共同处理")
	_, _, completed := find("101", slow, "completed")
	require.False(t, completed, "B 应在慢 A 结束前完成")
	for _, s := range []*integration.Suite{a, b} {
		for _, item := range read(s, "202").Receipts {
			require.NotEqual(t, slow, item.EventID, "不同用户不能读取慢任务记录")
		}
	}
	later := submit("101", "slow-2", 2, 0, true)
	require.Eventually(t, func() bool { _, _, ok := find("101", later, "completed"); return ok }, 6*time.Second, 10*time.Millisecond)
	var first, second receipt
	for _, s := range []*integration.Suite{a, b} {
		for _, item := range read(s, "101").Receipts {
			if item.EventID == slow && item.State == "completed" {
				first = item
			}
			if item.EventID == later && item.State == "completed" {
				second = item
			}
		}
	}
	require.False(t, first.CompletedAt.IsZero())
	require.GreaterOrEqual(t, second.StartedAt.UnixNano(), first.CompletedAt.UnixNano(), "同 key 后续任务不得越过慢/失败前序")
	require.GreaterOrEqual(t, second.Attempts, 2, "演示失败后必须重投才成功")
	t.Logf("发布服务+两个消费服务，处理 PID=%d/%d，instance=%s/%s", slowProgress.PID, fastProgress.PID, slowProgress.InstanceID, fastProgress.InstanceID)
}
