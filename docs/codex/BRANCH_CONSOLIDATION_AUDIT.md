# Core 分支收敛审计

## 基线

审计日期：2026-07-25

| 引用 | 提交 | 说明 |
| --- | --- | --- |
| `main` | `60e4216` | 当前唯一目标开发线，已包含分支收敛设计与实施计划 |
| `feat/web-runtime-auth` | `586f512` | `core-api-web` worktree 的待审计 tip |
| `optimize/code-cleanup` | `5eda9ad` | `feat/web-runtime-auth` 的历史分支基点 |
| `codex/optimize-code-cleanup` | `f97807f` | 已通过 `a13be18` 合入 `main` 的整理分支 |
| `main` / Web 共同基点 | `41da968` | `kin-openapi` 升级完成点 |

三个相关 worktree 在审计开始时均为干净状态。`main...feat/web-runtime-auth`
为 `40/69`；69 个旧分支提交不等同于 69 个缺失功能，必须按当前契约归类。

已确认末尾迁移映射：

| Web 提交 | main 提交 | 差异 |
| --- | --- | --- |
| `dbc4d3b` | `c79bee0` | 同一功能，摘取时调整 `web/admin` 子模块基点 |
| `7492e70` | `fcc3b31` | 同一嵌入产物发布目标，应用在不同基线 |
| `586f512` | `548aa53` | patch-id 完全等价 |

## 提交组去向

| 提交范围 | 功能组 | 分类 | main 证据 | 验证命令 |
| --- | --- | --- | --- | --- |
| `5dff1ed` | `web/admin` 子模块改用 HTTPS | 已合入 | 当前 `.gitmodules` 已使用 `https://github.com/bitzoom-futures/futures.admin.git` | `git show main:.gitmodules` |
| `53a81cd..1ff92b4` | Web runtime auth 设计、隔离分支说明与实施计划 | 需要补入 | 设计所要求的 `/api/web/bootstrap`、单一 Manage Auth 权威和同源认证代理在当前 `main` 均不存在；历史设计不能直接作为现行实现，补入时须以当前契约重写 | `git ls-tree main pkg/server/run/webbootstrap.go pkg/server/run/manageauth.go pkg/server/trans/rest/externalrouter.go` |
| `42f517d..206b914` | Manage Auth 权威、authority claim、HTML 同源认证代理和 bootstrap | 需要补入 | 当前 `main` 保留 `ManageAuth`/`ServerManageAuth` 与 Casdoor，但没有 `ManageAuthAuthorityService`、`manageauth.go`、`webbootstrap.go`、`externalrouter.go`；补入必须排除已经删除的 Logto、`AttachServices`、Observe/Notify | `rg -n "ManageAuthAuthority|webBootstrapPath|ExternalRouter" pkg/server` |
| `523b35c..c62f20e` | 可追溯 Web Admin 构建链、嵌入产物校验与部署说明 | 需要补入 | 当前嵌入产物和子模块最终指针可追溯且一致，但 `scripts/build-web-admin.sh`、`scripts/test-build-web-admin.sh` 不存在，`dist1` 旧备份仍存在，无法从源码稳定复建当前产物 | `git ls-tree main scripts/build-web-admin.sh scripts/test-build-web-admin.sh pkg/server/run/dist1`; `git show main:pkg/server/run/dist/build-info.json` |
| `48be84d..35bc8f4` | 前端同源 auth client 与 callback 白名单 | 已合入 | 当前 `main` 与旧分支均指向 `web/admin` 的 `0f7143e`，且 `build-info.json` 的 `frontend_commit`、`artifact_sha256` 完全一致 | `git ls-tree main web/admin`; `git diff main..feat/web-runtime-auth -- pkg/server/run/dist/build-info.json` |
| `1da3b35` | HTMLServer Manage 路径规范与完整安全链 | 需要补入 | 当前 `htmlHandler` 仍手工拆 URL、直接执行 Router，缺少旧提交中的统一认证、IP、解析和错误处理测试 | `git diff main..feat/web-runtime-auth -- pkg/server/run/htmlserver.go pkg/server/run/htmlserver_secure_routes_test.go` |
| `2b45346` | 菜单权限事务内原子替换 | 已合入 | `58aa32f` 建立稳定权限集合比较和错误传播，`7288163` 基于当前 `IDataAction` 实现整次菜单同步单事务；真实 SQLite 覆盖字段保留、权限替换、去重和第二个菜单失败时整体回滚 | `go test ./pkg/server/api/manage -count=1`; `go test -race ./pkg/server/api/manage -count=1` |
| `9dd1274` | 示例 04—07 的 Manage Auth 权威与启动配置 | 需要补入 | 当前示例没有单一 Manage Auth 权威；旧补丁同时包含已删除的 `RunIp` 等历史配置，不能原样摘取，只能随现行 runtime auth 设计适配 | `rg -n "ManageAuthAuthority" examples` |
| `08c4f2d` | 中间版 Admin 嵌入产物 | 已被替代 | 已被相同子模块最终指针和最终 `artifact_sha256` 覆盖 | `git show main:pkg/server/run/dist/build-info.json` |
| `23a27cb` | 示例 04/05 Casdoor 撤销与 Manage Auth peer 对齐 | 需要补入 | 当前示例缺少显式权威与 peer 对齐；补入时须使用当前 `AuthRevocation` 和已移除配置契约 | `git diff main..feat/web-runtime-auth -- examples/04-shop-performance/main/main.go examples/05-shop-casdoor-rbac/main/main.go` |
| `cac958d` | 中间版 Admin 嵌入产物 | 已被替代 | 已被最终可追溯产物覆盖 | `git show main:pkg/server/run/dist/build-info.json` |
| `dea0753` | 示例 07 要求权威服务实际提供 Manage | 需要补入 | 当前 runtime 尚无权威选择门禁，示例也没有对应断言 | `git show --stat dea0753` |
| `1a1bda4` | 中间版 Admin 嵌入产物 | 已被替代 | 已被最终可追溯产物覆盖 | `git show main:pkg/server/run/dist/build-info.json` |
| `cfa8c3b..1b5026b` | 示例 07 WS 就绪诊断及 Manage Auth ApplyShared/Sync 顺序 | 需要补入 | 当前示例缺少 `bootstrap/manageauth.go`；WS UAT 也没有这组确定性诊断和共享配置顺序覆盖 | `git diff main..feat/web-runtime-auth -- examples/07-shop-order-scale/bootstrap examples/integration/07-shop-order-scale/websocket_test.go` |
| `3242550..5cfac95` | 中间版 Admin 嵌入产物与菜单标题回退 | 已被替代 | 最终子模块指针和嵌入产物已包含后续状态 | `git ls-tree main web/admin`; `git show main:pkg/server/run/dist/build-info.json` |
| `f86e5fe` | 示例 04 命令选中行语义 | 已合入 | `3ffd6c5` 只为 Enable/Disable 设置选中行和确认，Add/Edit 等命令保留自身默认语义 | `go test ./examples/04-shop-performance/api/manage -count=1` |
| `6a4a15d` | 监听前同步执行 `IStartService.Start` | 已被替代 | 该方案在旧分支内先后被 revert/reapply/revert，最终由 `edc0edd` 的 listener 后 admission barrier 取代 | `git log --oneline 6a4a15d^..edc0edd -- pkg/server/run/server.go` |
| `49fe859..6a390e1` | 示例 07 WS/store 真实就绪与去盲重试 | 需要补入 | 当前 UAT 仍缺少该组基于可见产品和 store unavailable 的确定性就绪证据 | `git diff main..feat/web-runtime-auth -- examples/integration/07-shop-order-scale/websocket_test.go` |
| `7859766..d4e4e87` | 启动顺序方案的 revert/reapply/revert | 已被替代 | 三次提交净效果回到原语义，最终方案是后续 `edc0edd` | `git range-diff 6a4a15d^..d4e4e87 6a4a15d^..d4e4e87` |
| `edc0edd` | listener 后、业务请求前的 admission barrier | 需要补入 | 当前 `serviceStartContexts` 异步调用 hooks，REST 没有业务 admission middleware；监听就绪与业务可服务之间仍存在窗口 | `rg -n "BusinessAdmission|businessAdmission|Admission" pkg/server/router pkg/server/run pkg/server/trans/rest` |
| `459977e..7f9f4f2` | 示例 07 RuleCode fixture 原始读写、失败关闭与清理顺序 | 需要补入 | 当前缺少三个 fixture 逻辑/MySQL 测试文件，旧 UAT 对共享库恢复与失败关闭覆盖不足 | `git ls-tree main examples/integration/07-shop-order-scale/order_rule_fixture_logic_test.go examples/integration/07-shop-order-scale/order_rule_fixture_mysql_test.go` |
| `a7026e9` | Web Admin `npm ci` 禁用 Husky并同步产物 | 需要补入 | 最终产物已在 `main`，但当前没有可验证、不会触发宿主 hooks 的构建脚本 | `git ls-tree main scripts/build-web-admin.sh` |
| `9974e32..ac4550c` | IPv6/非法 Host 安全回退、Swagger 同源 Public/Private 挂载与 mux 冲突防护 | 需要补入 | 当前 `openapi.go` 未解析 Host/端口，`HTMLServer` 只挂 Swagger 静态资源和 Manage，不提供经过安全链的同源 Public/Private 代理 | `go test ./pkg/server/run -count=1`; `git diff main..feat/web-runtime-auth -- pkg/server/run/openapi.go pkg/server/run/htmlserver.go` |
| `b0a9ed9` | Manage View/Search/关联查询前端契约文档 | 需要补入 | 当前 `web/README.md` 没有这三类请求、分页、权限和只读子表约束 | `rg -n "Manage 查询能力|关联查询" web/README.md` |
| `ed1bbd9` | 项目级 Agent Skill 使用说明 | 已被替代 | 当前根 `AGENTS.md` 已显式加载 RTK，仓库内 `.codex/skills/use-digitalway-core` 及现行指南是有效入口；无需恢复旧 README 操作说明 | `test -f AGENTS.md`; `test -f .codex/skills/use-digitalway-core/SKILL.md` |
| `5efbe13` | 删除旧 `dist1` 并完善嵌入产物替换流程 | 需要补入 | 当前 `dist1` 仍存在，且构建/回滚/洁净检查脚本缺失 | `git ls-tree -r main pkg/server/run/dist1 | head` |
| `2de81b0..4179ef8` | 高级查询、错误保真、搜索与布局的前端源码和嵌入产物迭代 | 已合入 | 最终 `web/admin` 指针及 `build-info.json` 与旧分支一致；中间产物由最终产物替代 | `git ls-tree main web/admin`; `git diff main..feat/web-runtime-auth -- pkg/server/run/dist/build-info.json` |
| `9b4d475` | 保留 `decimal.Decimal` 搜索值 | 已合入 | `318868d` 在通用反射转换前使用 `decimal.NewFromString`，覆盖高精度文本、非法文本和 Manage `whereList` 转换 | `go test ./pkg/utils ./service/manage/view -count=1` |
| `36d264d..b76610a` | 菜单单栏查询、紧凑查询和属性选择器字段的前端迭代 | 已合入 | 最终子模块指针与最终嵌入产物在 `main` 和旧分支完全一致 | `git ls-tree main web/admin`; `git show main:pkg/server/run/dist/build-info.json` |
| `dbc4d3b..586f512` | 服务配置管理、最终 Admin 产物和 simple-shop auth authority 对齐 | 已合入 | 分别映射为 `c79bee0`、`fcc3b31`、`548aa53`；最后一项 patch-id 完全等价 | `git range-diff b76610a..586f512 72cc6cb..548aa53` |

分类只允许：已合入、已被替代、明确废弃、需要补入。

## 审计结论

- 69 个提交已按顺序由上表连续覆盖，没有把分支 ahead 数量误当成缺失功能数量。
- `web/admin` 的最终子模块指针和嵌入产物摘要已在 `main`，大部分前端中间提交无需再次合并。
- 旧分支仍保存四类有效缺口：Web runtime auth/HTML 安全链、Manage 正确性、启动与 UAT 门禁、可复现 Web 构建链。
- 旧分支还混有已删除的 Logto、`AttachServices`、Observe/Notify、`RunIp` 等历史契约。任何补入都必须从当前 `main` 重新设计和测试，禁止整体 merge 或直接 cherry-pick 提交组。
- 当前定向测试中 `pkg/server/api/manage`、`service/manage`、`pkg/server/api/public` 通过；`pkg/server/run` 首次在受限沙箱内因禁止绑定 `127.0.0.1` 失败，随后使用允许本地监听的执行环境复验通过。
- `go test ./examples/... ./pkg/server/integration -run "^$"` 已完成普通示例和 07 相关包编译；集成包 01—06 的 `TestMain` 在无测试匹配时仍申请连续端口，本环境返回“无法申请连续测试端口”，因此该命令不是完整 GREEN，也不影响“存在需要补入项、禁止清理”的结论。

### Manage 正确性补漏证据

2026-07-25 已在当前 `main` 完成 `2b45346`、`f86e5fe`、`9b4d475` 的现行契约适配：

- `318868d`：Decimal 高精度和非法输入按 RED→GREEN 修复。
- `3ffd6c5`：示例 04 只为 Enable/Disable 强制选中行。
- `58aa32f`、`7288163`：菜单稳定集合比较、错误传播和整次同步单事务。
- 定向测试：`go test ./pkg/utils ./service/manage/view ./examples/04-shop-performance/api/manage ./pkg/server/api/manage -count=1`，通过。
- Race：相同四个包执行 `go test -race`，通过。
- 全仓编译：`go test -p 1 ./... -run "^$" -count=1`，通过；顺序执行避免集成包并行争抢连续端口。
- 发布契约：`./scripts/test.sh release-contract`，通过，脚本未创建 tag、未 push、未发布。
- 全仓 `gofmt -l` 仍报告 18 个本批修改范围外的历史文件；本批新增和修改的 Go 文件均已格式化，未把无关格式债混入补漏提交。

## 清理门禁

- [x] 所有提交组均已分类
- [ ] 需要补入项为零或已进入 main
- [ ] 完整测试通过
- [ ] release-contract 通过
- [ ] archive tag 已验证
