# Manage Correctness Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在当前 `main` 上补回菜单原子同步、Decimal 查询转换和示例 04 命令选中行语义，并用回归测试解除三个旧提交的分支清理门禁。

**Architecture:** 三项修复彼此独立，按 Decimal、命令元数据、菜单同步三个小提交推进。菜单同步拆为纯比较/编排层与 `IDataAction` 事务适配层；一次 `UpdateMenu` 只开启一个事务，任何菜单或权限写入失败都回滚，并通过现有 Manage 响应链返回错误。

**Tech Stack:** Go 1.26、`shopspring/decimal`、现有 `ModelList`/`IDataAction`、GORM-backed SQLite 测试、`testify`、现有 release-contract。

---

### Task 1: 修复 Decimal 查询值转换

**Files:**
- Modify: `pkg/utils/conversion.go`
- Create: `pkg/utils/conversion_decimal_test.go`
- Create: `service/manage/view/model_test.go`

- [ ] **Step 1: 写 `AnyToTypeData` 的失败测试**

在 `pkg/utils/conversion_decimal_test.go` 增加合法高精度值和非法值测试：

```go
// 本文件验证 Manage 查询使用的 decimal.Decimal 转换保持精度并显式返回错误。
package utils

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestAnyToTypeDataPreservesDecimalText(t *testing.T) {
	got, err := AnyToTypeData("12345678901234567890.1234500", reflect.TypeOf(decimal.Decimal{}))
	require.NoError(t, err)
	value, ok := got.(decimal.Decimal)
	require.True(t, ok)
	require.True(t, value.Equal(decimal.RequireFromString("12345678901234567890.1234500")))
}

func TestAnyToTypeDataRejectsInvalidDecimalText(t *testing.T) {
	_, err := AnyToTypeData("not-a-decimal", reflect.TypeOf(decimal.Decimal{}))
	require.Error(t, err)
}
```

- [ ] **Step 2: 写查询模型链路的失败测试**

在 `service/manage/view/model_test.go` 构造 decimal 字段和 `whereList`，证明转换结果不是零值：

```go
// 本文件验证 Manage View 查询元数据到持久化 SearchItem 的类型转换。
package view

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSearchItemToSearchItemConvertsJSONNumberToDecimal(t *testing.T) {
	search := (&SearchItem{
		View: &ViewModel{Fields: []*FieldModel{{
			Field: "price", PropField: "Price", Type: "decimal",
			FieldType: reflect.TypeOf(decimal.Decimal{}),
		}}},
		WhereList: []*SearchWhere{{
			Name: "price", Symbol: ">", Value: float64(15),
		}},
	}).ToSearchItem()

	require.Len(t, search.WhereList, 1)
	where := search.WhereList[0]
	require.Equal(t, "Price", where.Column)
	require.Equal(t, ">", where.Symbol)
	value, ok := where.Value.(decimal.Decimal)
	require.True(t, ok)
	require.True(t, value.Equal(decimal.NewFromInt(15)))
}
```

- [ ] **Step 3: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils ./service/manage/view -run "TestAnyToTypeData|TestSearchItemToSearchItemConvertsJSONNumberToDecimal" -count=1
```

Expected: FAIL；合法 decimal 当前得到零值，非法 decimal 当前不会返回解析错误。

- [ ] **Step 4: 添加最小转换分支**

在 `AnyToTypeData` 中、调用 `convertOp1` 前增加：

```go
str := convertString(reflect.ValueOf(value))
if src == decimalType {
	return decimal.NewFromString(str)
}
rv, err := convertOp1(str, src)
```

不得将 decimal 先转换成 `float64`，其余基础类型分支保持不变。

- [ ] **Step 5: 运行定向测试确认 GREEN**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils ./service/manage/view -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交 Decimal 修复**

```bash
rtk git add pkg/utils/conversion.go pkg/utils/conversion_decimal_test.go service/manage/view/model_test.go
rtk git commit -m "fix(manage): preserve decimal query values"
```

### Task 2: 修复示例 04 命令选中行语义

**Files:**
- Modify: `examples/04-shop-performance/api/manage/base_data_manage.go`
- Modify: `examples/04-shop-performance/api/manage/inheritance_contract_test.go`

- [ ] **Step 1: 写命令元数据失败测试**

向 `inheritance_contract_test.go` 增加：

```go
func TestBaseDataManageViewCommandModelSelectRowSemantics(t *testing.T) {
	productManage := NewProductManage(nil)

	add := &view.CommandModel{Name: "Add", IsSelectRow: false, IsAlert: false}
	productManage.ViewCommandModel(add)
	assert.True(t, add.Visible)
	assert.False(t, add.IsSelectRow)
	assert.False(t, add.IsAlert)

	edit := &view.CommandModel{Name: "Edit", IsSelectRow: true, IsAlert: false}
	productManage.ViewCommandModel(edit)
	assert.True(t, edit.IsSelectRow)
	assert.False(t, edit.IsAlert)

	enable := &view.CommandModel{Name: "EnableBaseData"}
	productManage.ViewCommandModel(enable)
	assert.Equal(t, "启用", enable.Title)
	assert.True(t, enable.IsSelectRow)
	assert.True(t, enable.IsAlert)

	disable := &view.CommandModel{Name: "DisableBaseData"}
	productManage.ViewCommandModel(disable)
	assert.Equal(t, "禁用", disable.Title)
	assert.True(t, disable.IsSelectRow)
	assert.True(t, disable.IsAlert)
}
```

补充导入：

```go
"github.com/digitalwayhk/core/service/manage/view"
```

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/api/manage -run TestBaseDataManageViewCommandModelSelectRowSemantics -count=1
```

Expected: FAIL；Add 被当前实现改成 `IsSelectRow=true`、`IsAlert=true`。

- [ ] **Step 3: 只为 Enable/Disable 设置选择和确认**

将 `ViewCommandModel` 改为：

```go
// ViewCommandModel 配置通用启用和禁用按钮，不覆盖其他命令的默认选中行语义。
func (own *BaseDataManage[T]) ViewCommandModel(command *view.CommandModel) {
	if command == nil {
		return
	}
	command.Visible = true
	switch command.Name {
	case "EnableBaseData":
		command.Title = "启用"
		command.Icon = "check"
		command.IsSelectRow = true
		command.IsAlert = true
	case "DisableBaseData":
		command.Title = "禁用"
		command.Icon = "ban"
		command.IsSelectRow = true
		command.IsAlert = true
	}
}
```

- [ ] **Step 4: 运行示例 04 Manage 测试确认 GREEN**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/04-shop-performance/api/manage -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交命令语义修复**

```bash
rtk git add examples/04-shop-performance/api/manage/base_data_manage.go examples/04-shop-performance/api/manage/inheritance_contract_test.go
rtk git commit -m "fix(04): preserve manage command selection semantics"
```

### Task 3: 建立菜单同步的纯编排层

**Files:**
- Create: `pkg/server/api/manage/menu_sync.go`
- Create: `pkg/server/api/manage/menu_sync_test.go`
- Modify: `pkg/server/api/manage/menumanage.go`

- [ ] **Step 1: 写权限集合和字段保留失败测试**

`menu_sync_test.go` 至少包含：

```go
// 本文件验证菜单扫描结果的集合比较、用户字段保留和错误传播。
package manage

import (
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/stretchr/testify/require"
)

func TestPermissionSetsChangedDetectsSameLengthReplacement(t *testing.T) {
	old := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "edit", Url: "/api/manage/a/edit"},
	}
	next := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "remove", Url: "/api/manage/a/remove"},
	}
	require.True(t, permissionSetsChanged(old, next))
}

func TestPermissionSetsChangedIgnoresOrderAndDuplicates(t *testing.T) {
	old := []*smodels.PermissionsModel{
		{Name: "edit", Url: "/api/manage/a/edit"},
		{Name: "view", Url: "/api/manage/a/view"},
	}
	next := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "edit", Url: "/api/manage/a/edit"},
		{Name: "view", Url: "/api/manage/a/view"},
	}
	require.False(t, permissionSetsChanged(old, next))
}

func TestMergeGeneratedMenuPreservesUserFields(t *testing.T) {
	old := smodels.NewMenuModel()
	old.ID = 42
	old.DirectoryModelID = 9
	old.Title = "用户标题"
	old.Sort = 7
	old.Icon = "custom"
	old.Description = "用户说明"

	generated := smodels.NewMenuModel()
	generated.Name = "TokenManage"
	generated.Url = "/api/manage/token"

	merged := mergeGeneratedMenu(old, generated)
	require.Same(t, old, merged)
	require.Equal(t, uint(9), merged.DirectoryModelID)
	require.Equal(t, "用户标题", merged.Title)
	require.Equal(t, 7, merged.Sort)
	require.Equal(t, "custom", merged.Icon)
	require.Equal(t, "用户说明", merged.Description)
}

func TestUpdateMenuDoPropagatesSyncError(t *testing.T) {
	op := NewUpdateMenu(&MenuManage{})
	_, err := op.Do(nil)
	require.Error(t, err)
	require.False(t, errors.Is(err, nil))
}
```

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage -run "TestPermissionSetsChanged|TestMergeGeneratedMenu|TestUpdateMenuDoPropagatesSyncError" -count=1
```

Expected: FAIL，私有编排函数尚不存在，`UpdateMenu.Do` 仍吞掉同步失败。

- [ ] **Step 3: 实现稳定权限键与字段合并**

在 `menu_sync.go` 中定义：

```go
package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
)

func permissionKey(permission *smodels.PermissionsModel) string {
	if permission == nil {
		return ""
	}
	return strings.TrimSpace(permission.Name) + "\x00" + strings.TrimSpace(permission.Url)
}

func normalizedPermissionSet(items []*smodels.PermissionsModel) map[string]*smodels.PermissionsModel {
	result := make(map[string]*smodels.PermissionsModel, len(items))
	for _, item := range items {
		if key := permissionKey(item); key != "" {
			result[key] = item
		}
	}
	return result
}

func permissionSetsChanged(oldItems, newItems []*smodels.PermissionsModel) bool {
	oldSet := normalizedPermissionSet(oldItems)
	newSet := normalizedPermissionSet(newItems)
	if len(oldSet) != len(newSet) {
		return true
	}
	for key := range oldSet {
		if _, exists := newSet[key]; !exists {
			return true
		}
	}
	return false
}

func mergeGeneratedMenu(old, generated *smodels.MenuModel) *smodels.MenuModel {
	if old == nil {
		return generated
	}
	old.Name = generated.Name
	old.Url = generated.Url
	return old
}
```

`mergeGeneratedMenu` 必须复用 `old`，因此 ID、目录、标题、排序、图标和说明自然保留；不得把
生成对象整体覆盖到旧对象。

- [ ] **Step 4: 让菜单同步和 `UpdateMenu.Do` 返回错误**

将 `updateMenuModelAll` 签名改为：

```go
func (own *MenuManage) updateMenuModelAll(req types.IRequest) error
```

并将 `UpdateMenu.Do` 改为：

```go
func (own *UpdateMenu) Do(req types.IRequest) (interface{}, error) {
	if own.GetInstance() == nil {
		return nil, errors.New("UpdateMenu instance is nil")
	}
	mm, ok := own.GetInstance().(*MenuManage)
	if !ok {
		return nil, errors.New("UpdateMenu instance must be MenuManage")
	}
	if err := mm.updateMenuModelAll(req); err != nil {
		return nil, err
	}
	return nil, nil
}
```

本步骤只建立错误契约；真正持久化实现由 Task 4 接入。

- [ ] **Step 5: 运行定向测试**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage -run "TestPermissionSetsChanged|TestMergeGeneratedMenu|TestUpdateMenuDoPropagatesSyncError" -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交编排层**

```bash
rtk git add pkg/server/api/manage/menu_sync.go pkg/server/api/manage/menu_sync_test.go pkg/server/api/manage/menumanage.go
rtk git commit -m "refactor(manage): expose menu synchronization errors"
```

### Task 4: 用单事务同步全部菜单和权限

**Files:**
- Create: `pkg/server/api/manage/menu_persistence.go`
- Create: `pkg/server/api/manage/menu_persistence_test.go`
- Create: `pkg/server/api/manage/menu_persistence_fault_test.go`
- Modify: `pkg/server/api/manage/menumanage.go`

- [ ] **Step 1: 写事务生命周期和回滚失败测试**

测试使用真实临时 SQLite，并通过嵌入接口的包装器注入故障和统计事务调用：

```go
// 本文件使用真实 SQLite 和可控故障验证整次菜单同步只有一个事务边界。
package manage

type faultMenuAction struct {
	persistencetypes.IDataAction
	failInsertAt     int
	insertCalls      int
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

func (action *faultMenuAction) Commit() error {
	action.commitCalls++
	return action.IDataAction.Commit()
}

func (action *faultMenuAction) Rollback() error {
	action.rollbackCalls++
	return action.IDataAction.Rollback()
}
```

测试必须断言：

```go
func TestSyncMenusAtomicCommitsOnce(t *testing.T) {
	action, query := newMenuSQLiteAction(t)
	err := syncMenusAtomic(action, generatedMenusForTest())
	require.NoError(t, err)
	require.Equal(t, 1, action.transactionCalls)
	require.Equal(t, 1, action.commitCalls)
	require.Zero(t, action.rollbackCalls)
	require.Equal(t, []string{"FirstManage", "SecondManage"}, query.menuNames())
}

func TestSyncMenusAtomicRollsBackPermissionInsertFailure(t *testing.T) {
	action, query := newMenuSQLiteAction(t)
	seedExistingMenu(t, action.IDataAction)
	before := query.snapshot()
	action.failInsertAt = 2

	err := syncMenusAtomic(action, generatedMenusForTest())

	require.ErrorContains(t, err, "insert permission")
	require.Equal(t, 1, action.transactionCalls)
	require.Zero(t, action.commitCalls)
	require.Equal(t, 1, action.rollbackCalls)
	require.Equal(t, before, query.snapshot())
}
```

`newMenuSQLiteAction` 使用 `t.TempDir()`、`oltp.NewSqlite()` 和 `AutoMigrate` 创建隔离
数据库，返回包装后的 action 和只读查询 helper。`generatedMenusForTest` 固定生成两个
菜单及各自权限；把 `failInsertAt` 设在第二个菜单的权限写入位置，证明失败时第一个菜单也
不会残留。

- [ ] **Step 2: 运行测试确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage -run "TestSyncMenusAtomic" -count=1
```

Expected: FAIL，`syncMenusAtomic` 尚不存在。

- [ ] **Step 3: 实现事务包装器**

在 `menu_persistence.go` 顶部添加中文文件级注释，并实现严格的一次事务边界：

```go
func syncMenusAtomic(action persistencetypes.IDataAction, generated []*smodels.MenuModel) (err error) {
	if action == nil {
		return errors.New("menu persistence adapter unavailable")
	}
	if err := action.Transaction(); err != nil {
		return fmt.Errorf("begin menu sync transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := action.Rollback(); rollbackErr != nil && err == nil {
				err = fmt.Errorf("rollback menu sync transaction: %w", rollbackErr)
			}
		}
	}()

	for _, item := range generated {
		if item == nil {
			continue
		}
		if err := syncOneMenu(action, item); err != nil {
			return err
		}
	}
	if err := action.Commit(); err != nil {
		return fmt.Errorf("commit menu sync transaction: %w", err)
	}
	committed = true
	return nil
}
```

不得在 `syncOneMenu`、权限替换或新增菜单路径内再次调用
`Transaction`/`Commit`/`Rollback`。

- [ ] **Step 4: 实现同一 adapter 上的查询和替换**

`syncOneMenu` 必须：

1. 使用 `entity.NewModelList[smodels.MenuModel](action)` 和 `SearchOne` 按 `Name + Url`
   查询已有菜单并预加载权限。
2. 没有旧菜单时，为菜单及去重后的权限调用 `NewModel`、设置稳定 Hash，再 `Insert`。
3. 有旧菜单且权限集合相同则不写。
4. 有旧菜单且权限变化时，复用 `mergeGeneratedMenu(old, generated)`，通过
   `entity.NewModelList[smodels.PermissionsModel](action)` 查询旧权限。
5. `Update` 菜单标量、逐条 `Delete` 旧权限、逐条 `Insert` 新权限。

权限准备函数必须生成独立对象，避免修改路由扫描结果：

```go
func preparePermissions(menuID uint, items []*smodels.PermissionsModel) []*smodels.PermissionsModel {
	set := normalizedPermissionSet(items)
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]*smodels.PermissionsModel, 0, len(keys))
	for _, key := range keys {
		source := set[key]
		item := smodels.NewPermissionsModel()
		item.MenuModelID = menuID
		item.Name = source.Name
		item.Title = source.Title
		item.Description = source.Description
		item.Sort = source.Sort
		item.Icon = source.Icon
		item.Url = source.Url
		item.SetHashcode(utils.HashCodes(fmt.Sprintf(
			"%d|%s|%s", menuID, strings.TrimSpace(item.Name), strings.TrimSpace(item.Url),
		)))
		result = append(result, item)
	}
	return result
}
```

所有错误必须增加动作上下文后返回，例如：

```go
return fmt.Errorf("insert permission %s for menu %s: %w", permission.Name, menu.Name, err)
```

- [ ] **Step 5: 从 `MenuManage` 解析当前 adapter 并调用单事务同步**

`updateMenuModelAll` 必须验证 `DmpBase`、`ModelList` 和 adapter：

```go
func (own *MenuManage) updateMenuModelAll(req types.IRequest) error {
	if own == nil || own.DmpBase == nil {
		return errors.New("MenuManage list unavailable")
	}
	list, ok := own.GetList().(*entity.ModelList[smodels.MenuModel])
	if !ok || list == nil {
		return errors.New("MenuManage list unavailable")
	}
	search := list.GetSearchItem()
	search.Model = smodels.NewMenuModel()
	action := list.GetDBAdapter(search)
	if action == nil {
		action = list.GetAction()
	}
	return syncMenusAtomic(action, own.GetDefaultItemsWithRequest(req))
}
```

- [ ] **Step 6: 增加真实 SQLite 回滚测试**

使用临时 SQLite adapter 建表后：

1. 写入一个带用户标题和旧权限的菜单。
2. 注入包装过的 `IDataAction`，在第二次权限 `Insert` 返回固定错误。
3. 调用 `syncMenusAtomic`。
4. 重新查询数据库，断言标题、旧权限和权限数量完全不变。

测试文件使用 `t.TempDir()`，不写仓库目录或用户目录；MySQL 不参与此单元测试。

- [ ] **Step 7: 运行菜单测试确认 GREEN**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage -count=1
```

Expected: PASS。

- [ ] **Step 8: 运行菜单 race**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/api/manage -count=1
```

Expected: PASS。

- [ ] **Step 9: 提交原子菜单同步**

```bash
rtk git add pkg/server/api/manage/menu_persistence.go pkg/server/api/manage/menu_persistence_test.go pkg/server/api/manage/menu_persistence_fault_test.go pkg/server/api/manage/menumanage.go
rtk git commit -m "fix(manage): synchronize menus atomically"
```

### Task 5: 更新分支审计并运行补漏门禁

**Files:**
- Modify: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`

- [ ] **Step 1: 将三个提交组更新为已合入**

把 `2b45346`、`f86e5fe`、`9b4d475` 三行的分类改为“已合入”，并记录本批实际提交 SHA
和对应测试命令。其他“需要补入”行保持不变。

- [ ] **Step 2: 运行格式和定向测试**

```bash
rtk git ls-files "*.go" | rtk xargs gofmt -l
rtk git diff --check
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils ./service/manage/view ./examples/04-shop-performance/api/manage ./pkg/server/api/manage -count=1
```

Expected: `gofmt -l` 无输出，`git diff --check` 无错误，测试 PASS。

- [ ] **Step 3: 运行相关包 race**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils ./service/manage/view ./examples/04-shop-performance/api/manage ./pkg/server/api/manage -count=1
```

Expected: PASS。

- [ ] **Step 4: 运行全仓编译门禁**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -run "^$" -count=1
```

Expected: exit 0。若 `TestMain` 在无测试匹配时仍申请端口并失败，记录具体包、退出码和端口错误，
不得把定向 GREEN 报成全仓 GREEN。

- [ ] **Step 5: 运行发布契约**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: exit 0，且未创建 tag、未 push、未发布。

- [ ] **Step 6: 提交审计更新**

```bash
rtk git add docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
rtk git commit -m "docs: record manage correctness recovery"
```

- [ ] **Step 7: 确认仍不满足旧分支删除门禁**

Run:

```bash
rtk rg -n "\\| 需要补入 \\|" docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
```

Expected: 仍存在 runtime auth/HTML、启动/UAT 和 Web 构建链条目；保持
`feat/web-runtime-auth` worktree、分支和 tip 不变，不创建归档标签、不删除引用。
