# Manage 正确性补漏设计

## 背景

分支收敛审计确认，`feat/web-runtime-auth` 中有三项与 Web 认证无关、但尚未进入当前
`main` 的有效修复：

1. 菜单权限同步仍按权限数量判断变化，并通过多次独立写入更新数据。
2. `decimal.Decimal` 查询值经过通用反射转换时会得到零值。
3. 示例 04 为所有 Manage 命令无条件设置 `IsSelectRow=true`。

这些问题需要从当前 `main` 重新实现。禁止整体 merge 或直接 cherry-pick 旧提交，以免
带回已经删除的 Logto、`AttachServices`、Observe/Notify 或旧配置契约。

## 目标

- 菜单及其权限以单个数据库事务原子同步。
- 任一步失败时整体回滚，并通过 `UpdateMenu` 向调用方返回错误。
- 同数量但内容不同的权限也能正确更新。
- 同步框架生成字段时保留用户维护的菜单字段。
- `decimal.Decimal` 查询值精确解析，非法文本显式报错。
- 示例 04 只有启用、禁用命令强制要求选中行。
- 不改变 HTTP 路径、JSON 结构和现有公共 Go API。

## 非目标

- 不重构整个 Manage 持久化层。
- 不修改 Web Admin 页面或嵌入产物。
- 不处理 runtime auth、HTMLServer、OpenAPI、启动 admission barrier 或 UAT fixture。
- 不恢复旧分支的历史配置和已删除能力。

## 方案选择

采用“基于当前 `main` 重写最小补丁”。

不直接 cherry-pick：旧提交依赖不同的分支基线，容易混入已删除能力。也不借此重构整个
Manage 架构：本批目标是修复已经有明确证据的正确性问题，并尽快解除分支清理门禁。

## 菜单同步

### 比较规则

菜单的生成身份继续使用当前稳定业务键：

- 菜单：`Name + Url`。
- 权限：使用权限的稳定业务字段组成比较键，至少包含 `Name + Url`。

权限先按稳定键规范化和去重，再比较集合内容。比较结果不得依赖数据库返回顺序或路由注册
顺序，也不得只比较权限数量。

### 字段保留

找到已有菜单后，以现有记录为持久化主体，只更新由路由生成的字段和权限集合。用户维护的
目录归属、标题及其他非生成字段继续保留。新增菜单仍使用当前默认生成规则。

### 原子性

一次 `UpdateMenu` 操作在一个数据库事务内完成：

1. 查询已有菜单及权限。
2. 计算需要新增、替换或保持不变的菜单。
3. 保留已有菜单的用户字段。
4. 替换发生变化的权限集合。
5. 保存菜单及其关联数据。

任一查询、删除、写入或提交失败时，事务整体回滚。不得保留部分新菜单、部分旧权限或孤立
权限记录。

### 错误传播

内部同步函数返回 `error`。`UpdateMenu.Do` 将错误原样交给现有 Manage 响应链，不再只记
日志后返回成功。日志只记录稳定事件和必要上下文，不输出完整模型、权限载荷或数据库参数。

## Decimal 转换

通用转换在进入基础类型 `reflect.Kind` 分支前识别目标类型
`decimal.Decimal`，并调用 `decimal.NewFromString`：

- 合法整数、小数和高精度文本保持原值。
- 非法文本返回解析错误。
- 不改变现有整数、浮点数、布尔值和字符串的转换路径。
- 不把解析失败静默转换成零值。

## 示例 04 命令元数据

`BaseDataManage.ViewCommandModel` 继续设置通用显示属性，但只对
`EnableBaseData`、`DisableBaseData` 设置：

- `IsSelectRow=true`
- 对应标题和图标

其他命令不在该方法中强制改变 `IsSelectRow`，继续使用命令自身或上层 Manage 的默认
语义。

## 测试设计

### 菜单事务测试

- 权限数量相同、内容不同：旧权限被完整替换。
- 权限顺序变化、内容相同：不产生无意义更新。
- 重复生成权限：持久化结果去重。
- 同步已有菜单：目录、标题等用户字段保持不变。
- 菜单写入失败：菜单和权限均恢复到事务前状态。
- 权限替换失败：菜单和权限均恢复到事务前状态。
- `UpdateMenu.Do`：事务错误可由调用方观察，成功时返回现有成功形状。

故障测试通过当前持久化边界提供可控失败点，不引入只用于生产代码的全局开关。

### Decimal 测试

- `"123.4500"` 转换为等值 `decimal.Decimal`。
- 大整数和高精度小数不经过 `float64`。
- 非法文本返回错误。
- 原有基础类型转换回归通过。

### 命令元数据测试

- Enable/Disable 可见、需要选中行，并保留各自标题和图标。
- 普通 Add/Edit/Remove 或测试命令不被该方法强制设为需要选中行。

## 验证门禁

实现按 RED→GREEN 顺序推进，并至少执行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage ./service/manage/view ./examples/04-shop-performance/api/manage -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/api/manage ./service/manage/view ./examples/04-shop-performance/api/manage -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -run "^$" -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

若全仓命令因外部依赖或端口权限失败，必须记录命令、退出码和真实原因；定向 GREEN 不得冒充
全仓或发布 GREEN。

## 分支收敛衔接

完成并验证后，将 `BRANCH_CONSOLIDATION_AUDIT.md` 中以下提交组更新为“已合入”：

- `2b45346`
- `f86e5fe`
- `9b4d475`

其他“需要补入”组继续保持门禁，不能因此删除 `feat/web-runtime-auth` worktree 或分支。
