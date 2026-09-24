// 本文件用真实控制面模型验证 Core 内置管理员建档与角色解析。
package manageauth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestPrincipalProviderAssignsFirstAdminAndLaterViewer 验证并发首次登录只有一个系统管理员。
func TestPrincipalProviderAssignsFirstAdminAndLaterViewer(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action := oltp.NewFixedSqlite(fmt.Sprintf("manage_principal_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = action.DeleteDB() })
	runtime, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	provider := runtime.PrincipalProvider()

	requests := []servertype.ManagePrincipalRequest{
		managePrincipalCallback("manager-1", "alice"),
		managePrincipalCallback("manager-2", "bob"),
	}
	results := make(chan servertype.ManagePrincipal, len(requests))
	errorsCh := make(chan error, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		request := request
		wait.Add(1)
		go func() {
			defer wait.Done()
			principal, resolveErr := provider.ResolveManagePrincipal(context.Background(), request)
			results <- principal
			errorsCh <- resolveErr
		}()
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	for resolveErr := range errorsCh {
		require.NoError(t, resolveErr)
	}
	counts := map[string]int{}
	for principal := range results {
		require.Len(t, principal.Roles, 1)
		counts[principal.Roles[0].Code]++
	}
	require.Equal(t, 1, counts[servertype.ManageRoleSystemAdmin])
	require.Equal(t, 1, counts[servertype.ManageRoleViewer])
}

// TestPrincipalProviderRefreshRequiresExistingPrincipal 验证 refresh 不借机补建管理员。
func TestPrincipalProviderRefreshRequiresExistingPrincipal(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action := oltp.NewFixedSqlite(fmt.Sprintf("manage_principal_refresh_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = action.DeleteDB() })
	runtime, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)

	request := managePrincipalCallback("missing", "missing-subject")
	request.Source = servertype.AuthSourceRefresh
	_, err = runtime.PrincipalProvider().ResolveManagePrincipal(context.Background(), request)
	require.Error(t, err)
}

func managePrincipalCallback(uid, subject string) servertype.ManagePrincipalRequest {
	return servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{
			UID: uid, Username: uid, AuthType: servertype.AuthTypeManage,
			Provider: servertype.AuthProviderCasdoor, ProviderSubject: subject,
		},
		Source: servertype.AuthSourceCallback,
	}
}
