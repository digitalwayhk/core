// 本文件验证显式压测的身份与 pending 在测试清理后保留，普通夹具仍自动清理。
package shoporderscalemultiprocess

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeLoadStateSurvivesCleanup(t *testing.T) {
	t.Setenv("CORE_TEST_ORDER_LOAD", "1")
	t.Setenv("CORE_TEST_LOAD_EVIDENCE_DIR", t.TempDir())
	var record string
	t.Run("模拟压测退出", func(t *testing.T) {
		record = filepath.Join(nativeFixtureStateRoot(t), "pending.fact")
		require.NoError(t, os.WriteFile(record, []byte("已确认但尚未汇合的事实"), 0600))
	})
	data, err := os.ReadFile(record)
	require.NoError(t, err)
	require.Equal(t, "已确认但尚未汇合的事实", string(data))
}

func TestNativeOrdinaryStateIsCleaned(t *testing.T) {
	t.Setenv("CORE_TEST_ORDER_LOAD", "")
	var directory string
	t.Run("普通测试", func(t *testing.T) { directory = nativeFixtureStateRoot(t) })
	require.NoDirExists(t, directory)
}
