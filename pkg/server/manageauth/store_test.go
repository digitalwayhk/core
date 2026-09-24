// 本文件验证模型权限存储的精确查询条件和错误传播。
package manageauth

import (
	"context"
	"errors"
	"testing"

	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/stretchr/testify/require"
)

type roleListStub struct {
	items []*smodels.ManageRoleModel
	err   error
	field string
	value interface{}
}

func (s *roleListStub) SearchWhere(field string, value interface{}, _ ...func(*persistencetype.SearchItem)) ([]*smodels.ManageRoleModel, error) {
	s.field, s.value = field, value
	return s.items, s.err
}

type permissionListStub struct {
	items  []*smodels.ManageRolePermissionModel
	err    error
	fields map[string]interface{}
}

func (s *permissionListStub) SearchWhere(field string, value interface{}, callbacks ...func(*persistencetype.SearchItem)) ([]*smodels.ManageRolePermissionModel, error) {
	item := &persistencetype.SearchItem{}
	item.AddWhereN(field, value)
	for _, callback := range callbacks {
		callback(item)
	}
	s.fields = make(map[string]interface{}, len(item.WhereList))
	for _, where := range item.WhereList {
		s.fields[where.Column] = where.Value
	}
	return s.items, s.err
}

func TestModelStoreFindRoleQueriesStableCode(t *testing.T) {
	role := smodels.NewManageRoleModel()
	role.Code = "ops.approver"
	roles := &roleListStub{items: []*smodels.ManageRoleModel{role}}
	store := newModelStoreWithLists(func() manageRoleList { return roles }, nil)

	got, err := store.FindRole(context.Background(), role.Code)

	require.NoError(t, err)
	require.Same(t, role, got)
	require.Equal(t, "Code", roles.field)
	require.Equal(t, role.Code, roles.value)
}

func TestModelStorePermissionQueryUsesEveryExactDimension(t *testing.T) {
	permissions := &permissionListStub{items: []*smodels.ManageRolePermissionModel{nil}}
	store := newModelStoreWithLists(nil, func() manageRolePermissionList { return permissions })

	allowed, err := store.HasPermission(
		context.Background(),
		"ops.approver",
		"orders",
		"/api/manage/orders/ordermanage/approve",
		"approve",
	)

	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, map[string]interface{}{
		"RoleCode": "ops.approver",
		"Service":  "orders",
		"Path":     "/api/manage/orders/ordermanage/approve",
		"Command":  "approve",
	}, permissions.fields)
}

func TestModelStorePropagatesPersistenceErrors(t *testing.T) {
	cause := errors.New("query failed")
	store := newModelStoreWithLists(
		func() manageRoleList { return &roleListStub{err: cause} },
		func() manageRolePermissionList { return &permissionListStub{err: cause} },
	)

	_, err := store.FindRole(context.Background(), "ops.approver")
	require.ErrorIs(t, err, cause)
	_, err = store.HasPermission(context.Background(), "ops.approver", "orders", "/api/manage/orders/view", "view")
	require.ErrorIs(t, err, cause)
}
