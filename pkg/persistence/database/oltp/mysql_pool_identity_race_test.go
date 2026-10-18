// 本文件验证并发计算池身份和构造默认DSN时不修改共享配置。
package oltp

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

// TestMySQLPoolIdentityConcurrentDefaultConfig 验证零值默认化不会与并发身份读取发生竞争。
func TestMySQLPoolIdentityConcurrentDefaultConfig(t *testing.T) {
	config := Config{Host: "127.0.0.1", Port: 3306, Username: "user", Password: "secret"}
	before := config
	adapter := NewMySQL(&config)
	want := adapter.getConnectionKey()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				adapter.buildDSN()
				adapter.buildDSNWithDB("orders")
				adapter.getConnectionKey()
			}
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, before, config, "内部DSN构造必须只读共享配置")
	require.Equal(t, want, adapter.getConnectionKey())
}

// TestMySQLPoolIdentityDefaultConfigCreatesDatabase 验证未填字符集和时区的真实建库仍采用默认值且不回写输入。
func TestMySQLPoolIdentityDefaultConfigCreatesDatabase(t *testing.T) {
	base := newPoolRecoveryAdapter(t)
	config := *base.config
	config.Database = fmt.Sprintf("core_pool_defaults_%d", time.Now().UnixNano())
	config.Charset = ""
	config.Loc = ""
	config.ParseTime = false
	before := config
	adapter := NewMySQL(&config)
	var facts []*poolRecoveryFact
	require.NoError(t, adapter.Load(&types.SearchItem{Model: &poolRecoveryFact{}, Page: 1, Size: 1, SkipCount: true}, &facts))
	t.Cleanup(func() { connManager.Remove(adapter.getConnectionKey()) })
	require.Equal(t, before, config)
	var charset string
	require.NoError(t, adapter.db.Raw("SELECT @@character_set_database").Scan(&charset).Error)
	require.Equal(t, "utf8mb4", charset)
}
