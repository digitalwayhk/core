// 本文件验证 Manage 控制面 action、表初始化和内置角目录。
package manageauth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestNewControlPlaneActionUsesConfiguredSQLiteDatabase 验证系统模型自带的默认库名不会覆盖 server.json。
func TestNewControlPlaneActionUsesConfiguredSQLiteDatabase(t *testing.T) {
	database := fmt.Sprintf("control_plane_%d", time.Now().UnixNano())
	action, err := NewControlPlaneAction(config.ManageStoreConfig{
		Driver: config.ManageStoreDriverSQLite, Database: database,
		MaxIdleConns: 1, MaxOpenConns: 2,
	})
	require.NoError(t, err)

	cloner, ok := action.(interface {
		Clone() persistencetype.IDataAction
	})
	require.True(t, ok)
	cloneKeyed, ok := cloner.Clone().(interface{ GetSyncPoolKey(interface{}) string })
	require.True(t, ok)
	require.Contains(t, cloneKeyed.GetSyncPoolKey(smodels.NewManageRoleModel()), database)
	keyed, ok := action.(interface{ GetSyncPoolKey(interface{}) string })
	require.True(t, ok)
	require.Contains(t, keyed.GetSyncPoolKey(smodels.NewManageRoleModel()), database)

	if sqlite, ok := action.(*oltp.Sqlite); ok {
		t.Cleanup(func() { _ = sqlite.DeleteDB() })
	}
}

// TestControlPlaneRuntimeInitializesAllModelsAndBuiltInRoles 验证监听前完成表与默认目录初始化。
func TestControlPlaneRuntimeInitializesAllModelsAndBuiltInRoles(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	database := fmt.Sprintf("control_plane_runtime_%d", time.Now().UnixNano())
	action, err := NewControlPlaneAction(config.ManageStoreConfig{
		Driver: config.ManageStoreDriverSQLite, Database: database,
		MaxIdleConns: 1, MaxOpenConns: 2,
	})
	require.NoError(t, err)
	if sqlite, ok := action.(*oltp.Sqlite); ok {
		t.Cleanup(func() { _ = sqlite.DeleteDB() })
	}

	runtime, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	require.Same(t, action, runtime.Action())
	require.NotNil(t, runtime.Authorizer())

	dbValue, err := action.GetModelDB(smodels.NewManageRoleModel())
	require.NoError(t, err)
	db, ok := dbValue.(*gorm.DB)
	require.True(t, ok)
	for _, model := range []interface{}{
		smodels.NewDirectoryModel(),
		smodels.NewMenuModel(),
		smodels.NewPermissionsModel(),
		smodels.NewManageRoleModel(),
		smodels.NewManageRolePermissionModel(),
		smodels.NewManagePrincipalModel(),
		smodels.NewManagePrincipalRoleModel(),
	} {
		require.True(t, db.Migrator().HasTable(model), "missing table for %T", model)
	}

	for _, code := range []string{servertype.ManageRoleSystemAdmin, servertype.ManageRoleViewer} {
		role, findErr := runtime.Store().FindRole(context.Background(), code)
		require.NoError(t, findErr)
		require.NotNil(t, role)
		require.Equal(t, code, role.Code)
	}

	roles, err := entity.NewModelList[smodels.ManageRoleModel](action).SearchWhere("IsSystem", true)
	require.NoError(t, err)
	require.Len(t, roles, 2)

	second, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	require.NotNil(t, second)
	roles, err = entity.NewModelList[smodels.ManageRoleModel](action).SearchWhere("IsSystem", true)
	require.NoError(t, err)
	require.Len(t, roles, 2)
}

// TestControlPlaneRuntimeRejectsDifferentProcessStore 防止同一进程把 CRUD 与鉴权切到不同数据库。
func TestControlPlaneRuntimeRejectsDifferentProcessStore(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	firstName := fmt.Sprintf("control_plane_first_%d", time.Now().UnixNano())
	secondName := fmt.Sprintf("control_plane_second_%d", time.Now().UnixNano())
	first, err := NewControlPlaneAction(config.ManageStoreConfig{
		Driver: config.ManageStoreDriverSQLite, Database: firstName,
		MaxIdleConns: 1, MaxOpenConns: 2,
	})
	require.NoError(t, err)
	second, err := NewControlPlaneAction(config.ManageStoreConfig{
		Driver: config.ManageStoreDriverSQLite, Database: secondName,
		MaxIdleConns: 1, MaxOpenConns: 2,
	})
	require.NoError(t, err)
	for _, action := range []persistencetype.IDataAction{first, second} {
		if sqlite, ok := action.(*oltp.Sqlite); ok {
			t.Cleanup(func() { _ = sqlite.DeleteDB() })
		}
	}

	_, err = NewControlPlaneRuntime(first)
	require.NoError(t, err)
	_, err = NewControlPlaneRuntime(second)
	require.ErrorContains(t, err, "already configured")

	keyed, ok := managestore.Clone().(interface{ GetSyncPoolKey(interface{}) string })
	require.True(t, ok)
	require.Contains(t, keyed.GetSyncPoolKey(nil), firstName)
}

// TestControlPlaneRuntimeCreatesTablesDuringServerInitialization 验证控制面建表不会被服务初始化标记跳过。
func TestControlPlaneRuntimeCreatesTablesDuringServerInitialization(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action := oltp.NewFixedSqlite(fmt.Sprintf("control_plane_startup_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = action.DeleteDB() })
	config.BeginServerInitialization()
	t.Cleanup(config.EndServerInitialization)

	_, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	dbValue, err := action.GetModelDB(smodels.NewManagePrincipalModel())
	require.NoError(t, err)
	db, ok := dbValue.(*gorm.DB)
	require.True(t, ok)
	require.True(t, db.Migrator().HasTable(smodels.NewManagePrincipalModel()))
}

// TestControlPlaneRuntimeRejectsMissingBootstrapUniqueIndex 验证首管理员仲裁约束缺失时启动失败。
func TestControlPlaneRuntimeRejectsMissingBootstrapUniqueIndex(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action := oltp.NewFixedSqlite(fmt.Sprintf("control_plane_constraint_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = action.DeleteDB() })
	_, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	dbValue, err := action.GetModelDB(smodels.NewManagePrincipalModel())
	require.NoError(t, err)
	db := dbValue.(*gorm.DB)
	require.NoError(t, db.Migrator().DropIndex(smodels.NewManagePrincipalModel(), "BootstrapSlot"))
	managestore.ResetForTesting()

	_, err = NewControlPlaneRuntime(action)
	require.ErrorContains(t, err, "bootstrap unique index")
}

// TestControlPlaneRuntimeRejectsCompoundBootstrapUniqueIndex 验证复合唯一键不能冒充单列仲裁约束。
func TestControlPlaneRuntimeRejectsCompoundBootstrapUniqueIndex(t *testing.T) {
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
	action := oltp.NewFixedSqlite(fmt.Sprintf("control_plane_compound_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = action.DeleteDB() })
	_, err := NewControlPlaneRuntime(action)
	require.NoError(t, err)
	model := smodels.NewManagePrincipalModel()
	dbValue, err := action.GetModelDB(model)
	require.NoError(t, err)
	db := dbValue.(*gorm.DB)
	require.NoError(t, db.Migrator().DropIndex(model, "BootstrapSlot"))
	table := db.NamingStrategy.TableName("ManagePrincipalModel")
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_compound_bootstrap ON "+table+" (bootstrap_slot, code)").Error)
	managestore.ResetForTesting()

	_, err = NewControlPlaneRuntime(action)
	require.ErrorContains(t, err, "bootstrap unique index")
}

// TestNewControlPlaneActionRejectsUnsupportedDriver 验证工厂本身也 fail closed。
func TestNewControlPlaneActionRejectsUnsupportedDriver(t *testing.T) {
	_, err := NewControlPlaneAction(config.ManageStoreConfig{Driver: "unknown", Database: "core_manage"})
	require.Error(t, err)
	require.NotContains(t, strings.ToLower(err.Error()), "password")
}
