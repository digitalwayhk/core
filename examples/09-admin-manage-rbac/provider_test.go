// 本文件验证首用户、并发冲突、刷新和 TestToken 隔离等角色解析边界。
package adminrbac

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestProviderAssignsFirstCasdoorUserSystemAdminAndLaterUsersViewer(t *testing.T) {
	repository := newMemoryAdminRepository()
	provider := NewManageRoleProvider(repository)

	first, err := provider.ResolveManagePrincipal(context.Background(), callbackPrincipalRequest("user-1", "alice"))
	require.NoError(t, err)
	require.Equal(t, []servertype.ManageRoleRef{{Code: servertype.ManageRoleSystemAdmin}}, first.Roles)

	second, err := provider.ResolveManagePrincipal(context.Background(), callbackPrincipalRequest("user-2", "bob"))
	require.NoError(t, err)
	require.Equal(t, []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}, second.Roles)

	refreshed, err := provider.ResolveManagePrincipal(context.Background(), servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{
			UID: "user-2", AuthType: servertype.AuthTypeManage,
			Provider: servertype.AuthProviderCasdoor, ProviderSubject: "bob",
		},
		Source: servertype.AuthSourceRefresh,
	})
	require.NoError(t, err)
	require.Equal(t, second.Roles, refreshed.Roles)
	require.Equal(t, 2, repository.userCount())
}

func TestProviderConcurrentBootstrapCreatesExactlyOneSystemAdmin(t *testing.T) {
	repository := newMemoryAdminRepository()
	provider := NewManageRoleProvider(repository)
	requests := []servertype.ManagePrincipalRequest{
		callbackPrincipalRequest("user-1", "alice"),
		callbackPrincipalRequest("user-2", "bob"),
	}
	results := make(chan servertype.ManagePrincipal, len(requests))
	errorsCh := make(chan error, len(requests))
	var wait sync.WaitGroup
	for _, request := range requests {
		request := request
		wait.Add(1)
		go func() {
			defer wait.Done()
			principal, err := provider.ResolveManagePrincipal(context.Background(), request)
			results <- principal
			errorsCh <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		require.NoError(t, err)
	}
	adminCount := 0
	viewerCount := 0
	for principal := range results {
		require.Len(t, principal.Roles, 1)
		switch principal.Roles[0].Code {
		case servertype.ManageRoleSystemAdmin:
			adminCount++
		case servertype.ManageRoleViewer:
			viewerCount++
		}
	}
	require.Equal(t, 1, adminCount)
	require.Equal(t, 1, viewerCount)
}

func TestProviderRetriesBootstrapConflictAsViewerAcrossAuthorityInstances(t *testing.T) {
	repository := &bootstrapConflictRepository{}
	provider := NewManageRoleProvider(repository)

	principal, err := provider.ResolveManagePrincipal(
		context.Background(),
		callbackPrincipalRequest("user-2", "bob"),
	)

	require.NoError(t, err)
	require.Equal(t, []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}, principal.Roles)
	require.Equal(t, 2, repository.createCalls)
}

func TestProviderDoesNotAcceptTestTokenBootstrap(t *testing.T) {
	repository := newMemoryAdminRepository()
	provider := NewManageRoleProvider(repository)

	_, err := provider.ResolveManagePrincipal(context.Background(), servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{UID: "test-admin", AuthType: servertype.AuthTypeManage},
		Source:   servertype.AuthSourceTestToken,
	})

	require.Error(t, err)
	require.Zero(t, repository.userCount())
}

func TestProviderRefreshDoesNotCreateMissingUser(t *testing.T) {
	repository := newMemoryAdminRepository()
	provider := NewManageRoleProvider(repository)

	_, err := provider.ResolveManagePrincipal(context.Background(), servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{
			UID: "missing", AuthType: servertype.AuthTypeManage,
			Provider: servertype.AuthProviderCasdoor, ProviderSubject: "missing",
		},
		Source: servertype.AuthSourceRefresh,
	})

	require.Error(t, err)
	require.Zero(t, repository.userCount())
}

func TestProviderRoleChangeAppearsOnlyAfterPrincipalIsResolvedAgain(t *testing.T) {
	repository := newMemoryAdminRepository()
	provider := NewManageRoleProvider(repository)
	original, err := provider.ResolveManagePrincipal(context.Background(), callbackPrincipalRequest("user-1", "alice"))
	require.NoError(t, err)
	require.Equal(t, servertype.ManageRoleSystemAdmin, original.Roles[0].Code)

	repository.mu.Lock()
	repository.roles["user-1"] = []servertype.ManageRoleRef{{Code: "ops.approver"}}
	repository.mu.Unlock()

	// 已签发 token 使用原 principal 快照；角色关系变更不会反向修改它。
	require.Equal(t, servertype.ManageRoleSystemAdmin, original.Roles[0].Code)
	refreshed, err := provider.ResolveManagePrincipal(context.Background(), servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{
			UID: "user-1", AuthType: servertype.AuthTypeManage,
			Provider: servertype.AuthProviderCasdoor, ProviderSubject: "alice",
		},
		Source: servertype.AuthSourceRefresh,
	})
	require.NoError(t, err)
	require.Equal(t, []servertype.ManageRoleRef{{Code: "ops.approver"}}, refreshed.Roles)
}

func callbackPrincipalRequest(uid, subject string) servertype.ManagePrincipalRequest {
	return servertype.ManagePrincipalRequest{
		Identity: servertype.AuthIdentity{
			UID: uid, AuthType: servertype.AuthTypeManage,
			Provider: servertype.AuthProviderCasdoor, ProviderSubject: subject,
		},
		Source:       servertype.AuthSourceCallback,
		DefaultRoles: []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}},
	}
}

type bootstrapConflictRepository struct {
	user        *models.AdminUserModel
	createCalls int
}

func (own *bootstrapConflictRepository) FindUser(_ context.Context, _ string) (*models.AdminUserModel, error) {
	return own.user, nil
}

func (own *bootstrapConflictRepository) CreateUser(
	_ context.Context,
	user *models.AdminUserModel,
) ([]servertype.ManageRoleRef, error) {
	own.createCalls++
	if own.createCalls == 1 {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindConflict,
			servertype.PublicCodeConflict,
			"record already exists",
			errors.New("bootstrap slot already claimed"),
		)
	}
	user.IsFirst = false
	own.user = user
	return []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}, nil
}

func (own *bootstrapConflictRepository) RoleCodes(
	_ context.Context,
	_ string,
) ([]servertype.ManageRoleRef, error) {
	return []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}, nil
}
