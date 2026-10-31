//go:build integration

// 本文件验证 PostgreSQL 控制面并发首次登录只有一个系统管理员。
package manageauth

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestPostgresIntegration_FirstAdministrator 通过真实模型事务竞争首管理员唯一槽位。
func TestPostgresIntegration_FirstAdministrator(t *testing.T) {
	if os.Getenv("CORE_TEST_POSTGRES") != "1" {
		t.Skip("需要专用PostgreSQL")
	}
	c := types.StorageConfig{Driver: "postgres", Host: "127.0.0.1", Port: 15432, Username: "core_test", Password: "core_pg_test", Database: fmt.Sprintf("core_pg_admin_%d", time.Now().UnixNano()), SSLMode: "disable"}
	for key, target := range map[string]*string{"HOST": &c.Host, "USER": &c.Username, "PASSWORD": &c.Password} {
		if value := os.Getenv("CORE_TEST_POSTGRES_" + key); value != "" {
			*target = value
		}
	}
	if value := os.Getenv("CORE_TEST_POSTGRES_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		require.NoError(t, err)
		c.Port = port
	}
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action, err := oltp.NewDataAction(c)
	require.NoError(t, err)
	runtime, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	type outcome struct {
		principal servertype.ManagePrincipal
		err       error
	}
	results := make(chan outcome, 8)
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			principal, err := runtime.PrincipalProvider().ResolveManagePrincipal(context.Background(), managePrincipalCallback(fmt.Sprintf("manager-%d", i), fmt.Sprintf("subject-%d", i)))
			results <- outcome{principal, err}
		}(i)
	}
	wait.Wait()
	close(results)
	counts := map[string]int{}
	for result := range results {
		require.NoError(t, result.err)
		require.Len(t, result.principal.Roles, 1)
		counts[result.principal.Roles[0].Code]++
	}
	require.Equal(t, 1, counts[servertype.ManageRoleSystemAdmin])
	require.Equal(t, 7, counts[servertype.ManageRoleViewer])
}
