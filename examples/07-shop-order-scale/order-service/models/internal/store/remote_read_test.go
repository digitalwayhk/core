// 本文件验证单条权威只读查询使用独立 action，不开启事务且完整返回读取错误。
package store

import (
	"errors"
	"sync"
	"testing"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

type readOnlyAction struct {
	persistencetypes.IDataAction
	cloned bool
}

func (a *readOnlyAction) Clone() persistencetypes.IDataAction {
	return &readOnlyAction{cloned: true}
}

// TestRunRemoteRead 验证读连接隔离、回调错误透传且不调用事务方法。
func TestRunRemoteRead(t *testing.T) {
	oldAction := GetRemote()
	base := &readOnlyAction{}
	remoteAction = base
	defer func() { remoteAction = oldAction }()
	want := errors.New("读取失败")
	var seen []*readOnlyAction
	for range 2 {
		err := RunRemoteRead(func(action persistencetypes.IDataAction) error {
			reader := action.(*readOnlyAction)
			require.True(t, reader.cloned)
			require.NotSame(t, base, reader)
			seen = append(seen, reader)
			return want
		})
		require.ErrorIs(t, err, want)
	}
	require.NotSame(t, seen[0], seen[1])
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = RunRemoteRead(func(persistencetypes.IDataAction) error { return nil }) })
	}
	wg.Wait()
}
