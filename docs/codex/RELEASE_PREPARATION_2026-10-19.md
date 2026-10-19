# v1.3.0 之后的合并与发布准备

## 比较边界

- 分支：`feat/transactional-messaging-throughput`。审查起点 `5df05bbb2bc31a53e0e3691baa59cea64d844558`。
- 上版：`v1.3.0`，剥离 tag 对象后的提交为 `7cdf8d998d9ac54a9e880736284536d27380838b`，合并前本地与远端 main 一致。2026-10-19 只读 `git ls-remote` 核对通过；候选 `v1.4.0` 尚无远端 tag。
- 初始差异 48 个提交、331 个路径，其中 234 个 Go 文件；包含示例、测试、诊断工具、文档、后台子模块及内嵌产物，不能全部归为框架生产代码。
- 本次两项修复后代码提交固定为 `9a258cee5c6745c0de14050b767b0112eff62ebd`。随后发布准备提交只补文档；最终候选以该文档所在已提交树为准。
- 既有未跟踪 `web/scaffold-vue/` 和 `docs/codex/SCAFFOLD_VUE_CORE_INTEGRATION_STUDY.md` 排除于本轮，不删除、不暂存。没有修改、部署或重启其他项目。

建议采用 **v1.4.0 MINOR**：本轮包含显式启用的加性能力，官方 apidiff 对上版未报告已比较公共包的签名不兼容。MQ 缺省、安全行为、Badger 数据及原生错误身份仍有迁移影响；MINOR 不是无条件兼容承诺，升级须执行[消费方指导](UPGRADE_FROM_V1_3.md)。历史 Socket/OpenAPI 删除已经进入上版，不重复用历史批准推出本轮需要 MAJOR；也不能因为依赖 Badger 主版本变化就直接推断 Core 导出方法签名变化。

**本地合并已完成；正式发布结论仍为 NO-GO。** 按用户明确指令，本工作区已切换到 `main`，由 `7cdf8d9` 快进至 `c3b3ee85bc768632d193619f53e44e210db6ca5b`，纳入功能分支的 51 个提交；合并后树与功能分支完全一致，既有两处未跟踪 WIP 保留。没有新增代码、重跑测试、创建 tag 或 push。本地已有前端对象可用，合并不会将该对象上传到远端。此前将本地合并和正式发布一并阻断的表述已更正。

仍有一项未解除的功能回归失败：固定提交全仓回归的 07 WebSocket 场景首次下单返回 500。不能用初轮通过或前端产物检查通过代替该问题的处理与验证。

2026-10-20 更正发布边界：Core 已提交 gitlink 与内嵌产物均指向 `b705b5b49a879517e56f973e38c129039a10201c`，但远端空对象库 fetch 返回 `upload-pack: not our ref`。这个失败属实，影响前端源码维护与重建；`htmlserver.go` 通过 `//go:embed dist` 使用主仓库中已提交的 `pkg/server/run/dist`，普通 Go 构建、运行及 Core 发布不需要该子模块源码。此前将其列为 Core 发布阻断是审查边界错误，现已撤销，并同步发布策略与权威 skill。没有推送前端、修改源码、gitlink 或内嵌产物。

## 差异与修复项目

| 范围 | 相对上版结果 | 消费方影响 |
| --- | --- | --- |
| 稳定身份/注册 | 新增持久 InstanceID、独立 WorkerID 权威、Boot 隔离、注册恢复、准入/退出保护、离线恢复工具 | 默认 legacy，启用 stable 要先迁移身份、账本与历史时间边界；不能直接滚动克隆卷 |
| MQ/Event | 缺省 off；进程内事件保留；外部事件须显式启用 | 依赖旧缺省的服务必须补实际配置；多个 ServiceContext 无 MQ 自动互通的历史缺口仍开放 |
| 事务消息 | 可选 message 工具、事务 SQL 能力，06/07 接回统一入口，暴露 Outbox 批量设置 | 工具仍为 Experimental；业务表/投递状态/幂等保留期由项目确认，不自动建技术表 |
| 本地可靠写 | Badger v4.9.6、重复确认按实际队列移除扣减，准入/批提交观测 | 旧卷副本验证、保全升级后新事实的回退；不能清 pending 或宣称历史负数根因已修复 |
| SQL | MySQL 认证/驱动语义分池、配置默认化竞争/字节碰撞修复、物理故障与整池区分、SQLite 活动事务借连接修复 | 逐账号核对权限与连接总预算；写入结果未知不自动重放 |
| 查询/规则 | SearchItem 可选查询 ctx；07 Outbox 保留框架 Load 恢复；07/原生规则逐次读权威 | 已复制示例的项目合入修复；增加 SQL 查询，旧缓存性能数据不代表当前 |
| 管理端 | 补 RBAC 系统菜单与业务演示，权限树读写/回显/撤权，配套前端/内嵌产物 | 更新菜单并同步独立部署静态文件；TestToken 不创建真实管理员 |
| 依赖/工具 | go-zero v1.10.3；原生 07 对照、功能故障与性能证据工具 | 不把原生对照或临时测试参数推广成所有消费方默认 |

## 本轮审查发现并修复

1. **权限撤销跨页遗漏（P1）**：`PermissionSelection` 和 `replaceRolePermissions` 都使用默认 500 条 `SearchWhere`。实测保存 601 条后取消全部，仍残留 101 条。改为完整分页读取后事务替换，未放宽权限身份校验，也不改前端。提交 `ccc6b0c`；覆盖跨页回显、全部撤权、失败回滚及既有其他角色保持。
2. **stable 内部分发绕过准入（P1）**：同进程和 gRPC handler 走 `dispatchLocal → ParseNew → ExecDo`，没有经过 `RouterInfo.Exec` 的 HTTP 准入检查。实测未就绪时两入口仍返回成功。改为在共同分发入口验证内部身份后，解析结构化载荷前直接检查目标服务准入；失效拒绝，恢复放行，健康探针保留。提交 `9a258ce`；检查错调用方仍先返回身份拒绝。
3. **发布/升级说明漂移**：Unreleased 混有上版已发布内容，比较链接停在 v1.2.0；配置矩阵误写 MQ off 会关闭整个 EventBridge；README 把请求时建表描述成规范。已按实际 tag 分开本轮与历史，修正内存事件边界、启动前 EnsureStorage 要求和已知示例差距，集中新增消费方升级指导。
4. **配套前端提交未在远端可获取（源码维护限制，不阻断 Core 内嵌产物发布）**：后台远端 main 为 `7f8cdf58c96bd6c0bb365c6835386c3e1c914fd2`，不包含当前 gitlink。共享本地对象的裸库 fetch 曾返回成功，不能作为远端存在证明；完全空对象库的真实 fetch 返回明确的 `not our ref`。2026-10-20 重查仍失败，原始证据保留。只有需要从远端获取源码以修改、重建前端时才需先发布该提交；普通 Core 消费方使用已提交 dist，无需此步骤。未改前端代码、gitlink 或产物，未 push。
5. **固定提交真实回归发现 07 启动窗口下单 500（发布阻断，未标为已修复）**：`TestUATBuyerWebSocketOrderSubscription` 在 `create07BuyerOrder` 失败，尚未进入事件推送断言。日志中三个服务 `/api/ready` 已为 200，`service_hook_starting` 在首笔下单前后才出现，订单返回 HTTP 500 / `internal server error`。源码显示 legacy 的准入默认放行，而 `OrderWriteRuntime` 要到异步 `Service.Start` 才 Bind；测试的 ready+getproducts 也不能证明订单资源完成绑定。这与历史 R07 一致，但本次日志未保留完整业务 cause，不能直接宣布唯一根因或归因于本轮 stable 分发修复；该场景为 legacy，不能通过修改 stable 验收结论掩盖。原始失败封存为 `startup-window-failure.jsonl`。下一步需受控延迟 Bind、核对实际失败 cause，并在资源未就绪时拒绝 ready/业务准入或在监听前完成绑定，再回归；不能只增加 sleep、重跑通过或删除断言。

## 审查与验证

本仓库禁止子 Agent，本轮由主 Agent 直接审查。检查了核心变更及测试的正确性、失败路径、生命周期、认证域/权限、配置与持久数据兼容；未伪称独立第三方审计，也没有把大分支的全部文件都视为已穷尽审计。

证据位于本地 `.temp/release-preparation-20261019/`；下面状态按实际执行更新，不把旧提交测试或跳过项扩大为候选通过。

| 检查 | 结果与边界 |
| --- | --- |
| 远端 main/tag、分支祖先关系 | PASS，远端 main 与上版相同，分支包含 main，可快进；没有切换或修改其他 worktree |
| 上版精确 Go API 比较 | PASS，13 个受保护包加 nosql/oltp/authstate/routecache/utils 共 18 包，old/new 导出快照均成功，不兼容报告为空；工具固定为仓库 tools/go.mod 的官方 apidiff |
| 权限缺陷 RED | 已复现：601 条撤销后残留 101，`permissions-page-red.log`，不是夹具失败 |
| 内部分发 RED | 已复现：local/grpc-handler 未就绪时错误链为 nil，`internal-admission-red.log` |
| 权限/授权定向 race | PASS，18.650 秒，`permissions-green.json`；随后包含最终源码的扩大 race 继续通过 |
| 最终相关包 race | PASS，16 包、872 项测试，130.481 秒，`race.json`；包括真实独占 MySQL/Redis/etcd 的相关用例，不代表未启动的外部存储已验收 |
| 固定提交全仓功能回归 | FAIL，961.071 秒，132 包通过；2649 个测试/子测试通过、1 个测试失败、55 个跳过，失败包为 07 单进程集成。见 `full-candidate.json`，不能用初轮通过覆盖 |
| 发布候选脚本契约 | PASS，114.001 秒，`release.json`；包括 API/OpenAPI、公共 Go API、配置与安全检查。脚本范围通过不覆盖 07 真实回归失败 |
| 内嵌后台与日志门禁 | PASS，dist 同步 6.132 秒、日志 0.149 秒；均只读，不证明浏览器操作或子模块冷获取成功 |
| 无前端源码的 Go 构建（2026-10-20） | PASS，从 `a4e739a` 的主仓库 `git archive` 创建隔离检出，确认 `web/admin` 无源码后运行 `go build -mod=readonly ./pkg/server/run` 成功；`scripts/test.sh web-dist-sync` 契约及指针检查也通过。证据 `.temp/embedded-build-20261020.json`，不代表 07 功能失败已修复 |
| 子模块精确 SHA 远端冷获取 | FAIL，`b705b5b` 返回 `not our ref`；属于前端源码维护限制，不阻断使用已提交 dist 的 Core 发布 |
| 消费方升级/实际旧卷/浏览器/生产部署 | NOT_RUN，不动 Bitzoom，不以本仓库自测代替 |
| 性能容量 | 既有 2000/s × 600s 为 FAIL；本轮未运行负载或调参 |

API 对比不检测行为与数据格式：Badger v3 sentinel 比较、MQ 缺省、InitRequest.NewID 与池身份的迁移已另列指导。没有更新公共 API golden 来掩盖不兼容；13 包门禁与 v1.3.0 精确比较分别执行。

初轮全仓在审查/修复进行中启动，885.018 秒、133 包和2647个测试/子测试通过；它未包含后来新增的内部分发测试，不能当作最终候选结果。随后在独立干净副本固定 `9a258ce`、封存1439个Go源码/根依赖路径，重新运行全仓；两项新修复的用例通过，但上述07场景失败。所有原始日志保留，本轮未靠重复运行该失败场景取得绿灯。

`--release` 正式发布检查为 NOT_RUN：当前没有满足发布前置条件，不把 `--candidate` 脚本通过或一个干净检出伪装成整体可发布。该审查结束时尚未合并；之后已按用户指令完成上述本地快进合并，仍未创建 tag 或 push。

本轮仅创建带 `core.task=release-preparation-20261019` 标签的4个临时依赖：MySQL 数据 tmpfs 上限1GiB、Redis 32MiB、etcd/NATS 各128MiB，数据卷数为0。功能验证不代表主机断电耐久或生产旧卷兼容；验证后按精确 ID/名称/标签回收，证据见 `owned-resource-audit.json` 与 `cleanup.json`。没有使用或清理 Bitzoom 的 MySQL/Redis/容器。

## 仍开放的限制

- 原始负数 pending 的产生根因仍为 `NOT_REPRODUCED`；已验证的计数修复/恢复防护不代替根因关闭。
- 用户 facade 的幂等及消费去重集合长期增长仍缺业务保留期和持久权威，本轮不擅自 TTL；07 不代表生产持久 Inbox 已验收。
- 历史[示例审查](EXAMPLES_IMPLEMENTATION_REVIEW_2026-10-05.md)中未在本轮改动的 Manage Add Hook 错误、08 子行归属/父子原子性/复制、无 MQ 跨服务本地投递、01～05 启动屏障等仍开放。已有测试 PASS 不会关闭缺失覆盖；本次合并不能对外宣称框架和全部示例已无问题。
- stable 不是在途 SQL 的提交 fencing；需要外部隔离旧进程。Badger 人工兼容夹具不等于任意生产旧卷迁移；tmpfs 功能依赖不验证主机断电耐久。

## 合并与发布动作

候选、升级文档及代码已合入本地 main；本次仅同步合并状态，不创建 tag、不 push。既有前端研究 WIP 必须留在原工作区；正式发布检查应在候选的干净独立检出运行，不能 stash、删除 WIP 或绕过干净门禁。

先查清并修复、验证 07 启动窗口实际失败，再执行正式发布门禁。前端源码维护者若需远端重建，可另将已有 `b705b5b` 提交发布到 `.gitmodules` 对应仓库的可访问分支/tag，再从空对象库验证；该操作属于独立前端维护，不是 Core 发布前置条件，本轮没有执行。

正式发布前，维护者须重新核对远端 main；若远端已前进，先整合差异并回归受影响范围，再执行正式发布门禁。本地 main 已完成快进合并，不重复执行合并。`v1.4.0` 是建议版本，未发布前不得让消费方引用不存在的 tag。

人工发布约束来自 [发布策略](../RELEASE_POLICY.md)：“最后两条命令必须由发布负责人人工执行。发布脚本只读检查。”因此合并准备、正式只读门禁、人工创建 annotated tag/push 与实际消费方部署分别记录，不把文档完成等同于版本已上线。
