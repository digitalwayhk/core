// 本文件创建并初始化 Core Manage 控制面的唯一存储运行时。
package manageauth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"gorm.io/gorm"
)

// ControlPlaneRuntime 共享系统 Manage CRUD 与运行时鉴权使用的存储。
type ControlPlaneRuntime struct {
	action            persistencetype.IDataAction
	store             *ModelStore
	authorizer        *Authorizer
	principalProvider *PrincipalProvider
}

// NewControlPlaneAction 根据 server.json 的已验证配置创建控制面 action。
func NewControlPlaneAction(value config.ManageStoreConfig) (persistencetype.IDataAction, error) {
	value.ApplyDefaults()
	if err := value.Validate(); err != nil {
		return nil, err
	}
	switch value.Driver {
	case config.ManageStoreDriverSQLite:
		return oltp.NewFixedSqlite(value.Database), nil
	case config.ManageStoreDriverMySQL:
		return oltp.NewMySQL(&oltp.Config{
			Host:         value.Host,
			Port:         value.Port,
			Username:     value.Username,
			Password:     value.Password,
			Database:     value.Database,
			MaxIdleConns: value.MaxIdleConns,
			MaxOpenConns: value.MaxOpenConns,
		}), nil
	default:
		return nil, errors.New("unsupported manage store driver")
	}
}

// NewControlPlaneRuntime 在返回前完成全部表与内置角目录初始化。
func NewControlPlaneRuntime(action persistencetype.IDataAction) (*ControlPlaneRuntime, error) {
	if action == nil {
		return nil, errors.New("manage control-plane action is required")
	}
	store := NewModelStore(action)
	runtime := &ControlPlaneRuntime{
		action:            action,
		store:             store,
		authorizer:        NewAuthorizer(store),
		principalProvider: NewPrincipalProvider(store),
	}
	if err := runtime.EnsureStorage(); err != nil {
		return nil, err
	}
	if err := managestore.Configure(action); err != nil {
		return nil, err
	}
	return runtime, nil
}

// PrincipalProvider 返回 Core 默认的 Manage 管理员角色解析器。
func (own *ControlPlaneRuntime) PrincipalProvider() *PrincipalProvider {
	if own == nil {
		return nil
	}
	return own.principalProvider
}

// Action 返回系统 Manage CRUD 必须共用的数据操作器。
func (own *ControlPlaneRuntime) Action() persistencetype.IDataAction {
	if own == nil {
		return nil
	}
	return own.action
}

// Store 返回自定义角色与精确权限查询存储。
func (own *ControlPlaneRuntime) Store() *ModelStore {
	if own == nil {
		return nil
	}
	return own.store
}

// Authorizer 返回 Manage REST 请求使用的授权器。
func (own *ControlPlaneRuntime) Authorizer() *Authorizer {
	if own == nil {
		return nil
	}
	return own.authorizer
}

// EnsureStorage 在 HTTP/gRPC 监听前建立全部控制面表和内置角目录。
func (own *ControlPlaneRuntime) EnsureStorage() error {
	if own == nil || own.action == nil {
		return errors.New("manage control-plane action is required")
	}
	database, ok := own.action.(persistencetype.IDataBase)
	if !ok {
		return errors.New("manage control-plane action cannot initialize tables")
	}
	models := []struct {
		name  string
		model interface{}
	}{
		{name: "directories", model: smodels.NewDirectoryModel()},
		{name: "menus", model: smodels.NewMenuModel()},
		{name: "commands", model: smodels.NewPermissionsModel()},
		{name: "roles", model: smodels.NewManageRoleModel()},
		{name: "role permissions", model: smodels.NewManageRolePermissionModel()},
		{name: "principals", model: smodels.NewManagePrincipalModel()},
		{name: "principal roles", model: smodels.NewManagePrincipalRoleModel()},
	}
	for _, item := range models {
		if err := database.HasTable(item.model); err != nil {
			return fmt.Errorf("initialize manage %s: %w", item.name, err)
		}
	}
	if err := own.verifyBootstrapConstraint(); err != nil {
		return err
	}
	return own.ensureBuiltInRoles()
}

func (own *ControlPlaneRuntime) verifyBootstrapConstraint() error {
	model := smodels.NewManagePrincipalModel()
	dbValue, err := own.action.GetModelDB(model)
	if err != nil {
		return fmt.Errorf("verify manage principal bootstrap constraint: %w", err)
	}
	db, ok := dbValue.(*gorm.DB)
	if !ok || db == nil {
		return errors.New("verify manage principal bootstrap constraint: unsupported database handle")
	}
	if !db.Migrator().HasColumn(model, "BootstrapSlot") {
		return errors.New("manage principal bootstrap column is missing")
	}
	indexes, err := db.Migrator().GetIndexes(model)
	if err != nil {
		return fmt.Errorf("verify manage principal bootstrap indexes: %w", err)
	}
	for _, index := range indexes {
		unique, known := index.Unique()
		columns := index.Columns()
		if !known || !unique || len(columns) != 1 {
			continue
		}
		if strings.EqualFold(columns[0], "bootstrap_slot") {
			return nil
		}
	}
	return errors.New("manage principal bootstrap unique index is missing")
}

func (own *ControlPlaneRuntime) ensureBuiltInRoles() error {
	list := entity.NewModelList[smodels.ManageRoleModel](own.action)
	for _, role := range smodels.NewBuiltInManageRoles() {
		existing, err := list.SearchOne(func(search *persistencetype.SearchItem) {
			search.AddWhereN("Code", role.Code)
		})
		if err != nil {
			return fmt.Errorf("query built-in manage role %s: %w", role.Code, err)
		}
		if existing != nil {
			continue
		}
		if err := list.Add(role); err != nil {
			return fmt.Errorf("prepare built-in manage role %s: %w", role.Code, err)
		}
		if err := list.Save(); err != nil {
			if servertype.ResolvePublicError(err).Kind == servertype.ErrorKindConflict {
				stored, findErr := own.store.FindRole(nil, role.Code)
				if findErr == nil && stored != nil {
					continue
				}
			}
			return fmt.Errorf("save built-in manage role %s: %w", role.Code, err)
		}
	}
	return nil
}
