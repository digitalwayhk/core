package casdoorrbacshop

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type claimRecorder map[string]string

func (own claimRecorder) AddData(key, value string) { own[key] = value }

func TestShopServiceOnAuthDerivesRoleFromTrustedAuthType(t *testing.T) {
	service := &ShopService{}
	tests := []struct {
		name     string
		authType types.AuthType
		role     string
		scope    string
	}{
		{name: "普通用户", authType: types.AuthTypeUser, role: "user", scope: "order"},
		{name: "管理员", authType: types.AuthTypeManage, role: "administrator", scope: "manage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := claimRecorder{}
			err := service.OnAuth(context.Background(), &types.AuthHookArgs{
				UID: "user-1", AuthType: tt.authType, Claims: claims,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.role, claims["role"])
			assert.Equal(t, tt.scope, claims["shop_scope"])
			assert.Equal(t, contract.ServiceName, claims["example_service"])
		})
	}
}

func TestShopServiceOnAuthFailsClosedForUnsupportedType(t *testing.T) {
	service := &ShopService{}
	err := service.OnAuth(context.Background(), &types.AuthHookArgs{
		UID: "server-manager", AuthType: types.AuthTypeServerManage, Claims: claimRecorder{},
	})
	require.Error(t, err)
	contract := types.ResolvePublicError(err)
	assert.Equal(t, types.ErrorKindForbidden, contract.Kind)
	assert.Equal(t, "权限不足", contract.Message)
}

func TestShopServiceOnAuthRequestEnforcesRoleAndScope(t *testing.T) {
	service := &ShopService{}
	require.NoError(t, service.OnAuthRequest(context.Background(), types.AuthRequestArgs{
		Identity: types.AuthIdentity{UID: "user-1", AuthType: types.AuthTypeUser},
		PathType: types.PrivateType,
		Claims:   map[string]interface{}{"role": "user", "shop_scope": "order"},
	}))
	require.NoError(t, service.OnAuthRequest(context.Background(), types.AuthRequestArgs{
		Identity: types.AuthIdentity{UID: "admin-1", AuthType: types.AuthTypeManage},
		PathType: types.ManageType,
		Claims:   map[string]interface{}{"role": "administrator", "shop_scope": "manage"},
	}))

	err := service.OnAuthRequest(context.Background(), types.AuthRequestArgs{
		Identity: types.AuthIdentity{UID: "user-1", AuthType: types.AuthTypeUser},
		PathType: types.ManageType,
		Claims:   map[string]interface{}{"role": "administrator", "shop_scope": "manage"},
	})
	require.Error(t, err)
	assert.Equal(t, types.ErrorKindForbidden, types.ResolvePublicError(err).Kind)
}

func TestShopServiceOnCasdoorEventPersistsSanitizedAudit(t *testing.T) {
	utils.TESTPATH = t.TempDir()
	service := &ShopService{}
	event := types.CasdoorEvent{
		ID: "hook-event-001", ServiceName: contract.ServiceName,
		AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor,
		ProviderSubject: "alice", UID: "user-1", EventType: "logout",
		EventOrder: 1_700_000_001, Generation: 4, Blocked: true,
		OccurredAt: time.Unix(1_700_000_001, 0).UTC(),
	}

	require.NoError(t, service.OnCasdoorEvent(context.Background(), event))
	require.NoError(t, service.OnCasdoorEvent(context.Background(), event))
	records, err := models.NewIdentityEventRecord().QueryByEventID(event.ID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, event.UID, records[0].UserID)
}

var (
	_ types.IAuthHookProvider         = (*ShopService)(nil)
	_ types.IAuthRequestHookProvider  = (*ShopService)(nil)
	_ types.ICasdoorEventHookProvider = (*ShopService)(nil)
)
