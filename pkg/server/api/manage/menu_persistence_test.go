// 本文件使用真实 SQLite 和可控故障验证整次菜单同步只有一个事务边界。
package manage

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type faultMenuAction struct {
	persistencetypes.IDataAction
	failInsertAt     int
	insertCalls      int
	updateCalls      int
	deleteCalls      int
	transactionCalls int
	commitCalls      int
	rollbackCalls    int
}

func (action *faultMenuAction) Transaction() error {
	action.transactionCalls++
	return action.IDataAction.Transaction()
}

func (action *faultMenuAction) Insert(data interface{}) error {
	action.insertCalls++
	if action.failInsertAt > 0 && action.insertCalls == action.failInsertAt {
		return errors.New("injected insert failure")
	}
	return action.IDataAction.Insert(data)
}

func (action *faultMenuAction) Update(data interface{}) error {
	action.updateCalls++
	return action.IDataAction.Update(data)
}

func (action *faultMenuAction) Delete(data interface{}) error {
	action.deleteCalls++
	return action.IDataAction.Delete(data)
}

func (action *faultMenuAction) Commit() error {
	action.commitCalls++
	return action.IDataAction.Commit()
}

func (action *faultMenuAction) Rollback() error {
	action.rollbackCalls++
	return action.IDataAction.Rollback()
}

func newMenuSQLiteAction(t *testing.T) (*faultMenuAction, *gorm.DB) {
	t.Helper()

	previousTestPath := utils.TESTPATH
	utils.TESTPATH = filepath.Clean(t.TempDir()) + string(filepath.Separator)
	t.Cleanup(func() {
		utils.TESTPATH = previousTestPath
	})

	adapter := oltp.NewSqlite()
	adapter.Name = "models"
	adapter.IsLog = false
	db, err := adapter.GetDB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&smodels.MenuModel{},
		&smodels.PermissionsModel{},
		&smodels.DirectoryModel{},
	))
	return &faultMenuAction{IDataAction: adapter}, db
}

func generatedMenu(name string, permissions ...string) *smodels.MenuModel {
	menu := smodels.NewMenuModel()
	menu.Name = name
	menu.Url = "/api/manage/" + strings.ToLower(name)
	for _, permissionName := range permissions {
		permission := smodels.NewPermissionsModel()
		permission.Name = permissionName
		permission.Url = menu.Url + "/" + permissionName
		menu.Permissions = append(menu.Permissions, permission)
	}
	return menu
}

func seedExistingMenu(t *testing.T, db *gorm.DB) *smodels.MenuModel {
	t.Helper()

	menu := generatedMenu("TokenManage", "view", "edit")
	menu.Title = "用户标题"
	menu.DirectoryModelID = 9
	menu.Sort = 7
	menu.Icon = "custom"
	menu.Description = "用户说明"
	menu.SetHashcode(menu.GetHash())
	permissions := menu.Permissions
	menu.Permissions = nil
	require.NoError(t, db.Create(menu).Error)
	for _, permission := range permissions {
		permission.MenuModelID = menu.ID
		permission.SetHashcode(utils.HashCodes(permission.Url))
		require.NoError(t, db.Create(permission).Error)
	}
	menu.Permissions = permissions
	return menu
}

func loadMenuNames(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var menus []*smodels.MenuModel
	require.NoError(t, db.Order("name ASC").Find(&menus).Error)
	names := make([]string, 0, len(menus))
	for _, menu := range menus {
		names = append(names, menu.Name)
	}
	return names
}

func loadPermissionNames(t *testing.T, db *gorm.DB, menuID uint) []string {
	t.Helper()
	var permissions []*smodels.PermissionsModel
	require.NoError(t, db.Where("menu_model_id = ?", menuID).Order("name ASC").Find(&permissions).Error)
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, permission.Name)
	}
	return names
}

func TestSyncMenusAtomicCommitsOnce(t *testing.T) {
	action, db := newMenuSQLiteAction(t)

	err := syncMenusAtomic(action, []*smodels.MenuModel{
		generatedMenu("FirstManage", "view"),
		generatedMenu("SecondManage", "view"),
	})

	require.NoError(t, err)
	require.Equal(t, 1, action.transactionCalls)
	require.Equal(t, 1, action.commitCalls)
	require.Zero(t, action.rollbackCalls)
	require.Equal(t, []string{"FirstManage", "SecondManage"}, loadMenuNames(t, db))
}

func TestSyncMenusAtomicRollsBackSecondMenuFailure(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	action.failInsertAt = 4

	err := syncMenusAtomic(action, []*smodels.MenuModel{
		generatedMenu("FirstManage", "view"),
		generatedMenu("SecondManage", "view"),
	})

	require.ErrorContains(t, err, "insert permission")
	require.Equal(t, 1, action.transactionCalls)
	require.Zero(t, action.commitCalls)
	require.Equal(t, 1, action.rollbackCalls)
	require.Empty(t, loadMenuNames(t, db))
}

func TestSyncMenusAtomicReplacesPermissionsAndPreservesUserFields(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	existing := seedExistingMenu(t, db)

	err := syncMenusAtomic(action, []*smodels.MenuModel{
		generatedMenu("TokenManage", "view", "remove"),
	})

	require.NoError(t, err)
	var reloaded smodels.MenuModel
	require.NoError(t, db.First(&reloaded, existing.ID).Error)
	require.Equal(t, "用户标题", reloaded.Title)
	require.Equal(t, uint(9), reloaded.DirectoryModelID)
	require.Equal(t, 7, reloaded.Sort)
	require.Equal(t, "custom", reloaded.Icon)
	require.Equal(t, "用户说明", reloaded.Description)
	require.Equal(t, []string{"remove", "view"}, loadPermissionNames(t, db, existing.ID))
}

func TestSyncMenusAtomicSkipsEquivalentPermissionSet(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	existing := seedExistingMenu(t, db)

	err := syncMenusAtomic(action, []*smodels.MenuModel{
		generatedMenu("TokenManage", "edit", "view", "view"),
	})

	require.NoError(t, err)
	require.Zero(t, action.updateCalls)
	require.Zero(t, action.deleteCalls)
	require.Equal(t, []string{"edit", "view"}, loadPermissionNames(t, db, existing.ID))
}

// 存量菜单最常见的情形：权限一个没变，代码里刚补了中英标题。
// 早期实现在权限未变时直接 return，标题永远刷不进库。
func TestSyncMenusAtomicRefreshesTitlesWhenPermissionsUnchanged(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	existing := seedExistingMenu(t, db)

	generated := generatedMenu("TokenManage", "edit", "view")
	generated.Title = "代币管理"
	generated.TitleEN = "Tokens"

	require.NoError(t, syncMenusAtomic(action, []*smodels.MenuModel{generated}))

	var reloaded smodels.MenuModel
	require.NoError(t, db.First(&reloaded, existing.ID).Error)
	require.Equal(t, "代币管理", reloaded.Title)
	require.Equal(t, "Tokens", reloaded.TitleEN)
	// 权限未变时不得重建权限行，只写展示标题
	require.Equal(t, 1, action.updateCalls)
	require.Zero(t, action.deleteCalls)
	require.Equal(t, []string{"edit", "view"}, loadPermissionNames(t, db, existing.ID))
}

// 刷新标题不得顺手覆盖用户在「菜单管理」里排的序和挑的图标。
func TestSyncMenusAtomicKeepsUserFieldsWhileRefreshingTitles(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	existing := seedExistingMenu(t, db)

	generated := generatedMenu("TokenManage", "edit", "view")
	generated.Title = "代币管理"
	generated.TitleEN = "Tokens"
	generated.Sort = 0
	generated.Icon = ""
	generated.Description = ""

	require.NoError(t, syncMenusAtomic(action, []*smodels.MenuModel{generated}))

	var reloaded smodels.MenuModel
	require.NoError(t, db.First(&reloaded, existing.ID).Error)
	require.Equal(t, "代币管理", reloaded.Title)
	require.Equal(t, "Tokens", reloaded.TitleEN)
	require.Equal(t, 7, reloaded.Sort)
	require.Equal(t, "custom", reloaded.Icon)
	require.Equal(t, "用户说明", reloaded.Description)
	require.Equal(t, uint(9), reloaded.DirectoryModelID)
}

// 实现 ILocaleTitle 的新 Manage 首次插入即带中英标题。
func TestSyncMenusAtomicInsertsBothTitlesForNewMenu(t *testing.T) {
	action, db := newMenuSQLiteAction(t)

	generated := generatedMenu("TokenManage", "view")
	generated.Title = "代币管理"
	generated.TitleEN = "Tokens"

	require.NoError(t, syncMenusAtomic(action, []*smodels.MenuModel{generated}))

	var reloaded smodels.MenuModel
	require.NoError(t, db.Where("name = ?", "TokenManage").First(&reloaded).Error)
	require.Equal(t, "代币管理", reloaded.Title)
	require.Equal(t, "Tokens", reloaded.TitleEN)
}

func seedDirectory(t *testing.T, db *gorm.DB, name, title, titleEN string) *smodels.DirectoryModel {
	t.Helper()
	directory := smodels.NewDirectoryModel()
	directory.Name = name
	directory.Title = title
	directory.TitleEN = titleEN
	directory.Sort = 5
	directory.Icon = "folder"
	directory.SetHashcode(directory.GetHash())
	require.NoError(t, db.Create(directory).Error)
	return directory
}

func generatedDirectory(name, title, titleEN string) *smodels.DirectoryModel {
	directory := smodels.NewDirectoryModel()
	directory.Name = name
	directory.Title = title
	directory.TitleEN = titleEN
	return directory
}

// 目录与菜单同一规则：存量目录也必须能拿到代码里新写的英文标题。
func TestSyncDirectoryTitlesRefreshesExistingRows(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	existing := seedDirectory(t, db, "demo", "演示服务", "")

	require.NoError(t, syncDirectoryTitles(action, []*smodels.DirectoryModel{
		generatedDirectory("demo", "演示服务", "Demo"),
	}))

	var reloaded smodels.DirectoryModel
	require.NoError(t, db.First(&reloaded, existing.ID).Error)
	require.Equal(t, "演示服务", reloaded.Title)
	require.Equal(t, "Demo", reloaded.TitleEN)
	require.Equal(t, 5, reloaded.Sort)
	require.Equal(t, "folder", reloaded.Icon)
}

// 标题一致时不写库；未落库的服务目录不由这条路径创建。
func TestSyncDirectoryTitlesSkipsUnchangedAndMissingRows(t *testing.T) {
	action, db := newMenuSQLiteAction(t)
	seedDirectory(t, db, "demo", "演示服务", "Demo")

	require.NoError(t, syncDirectoryTitles(action, []*smodels.DirectoryModel{
		generatedDirectory("demo", "演示服务", "Demo"),
		generatedDirectory("absent", "缺席服务", "Absent"),
	}))

	require.Zero(t, action.updateCalls)
	var total int64
	require.NoError(t, db.Model(&smodels.DirectoryModel{}).Count(&total).Error)
	require.Equal(t, int64(1), total)
}
