# RouterInfo 运行时解耦整计划最终外部审查提示词

请对 RouterInfo 运行时解耦整份计划做一次只读终审，不要修改代码、文档、Git 状态或基线。

## 审查范围

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex
BASE=7631d78c4e7541d1ea431adf8e1b5512d5186014
FINAL=$(git rev-parse HEAD)
git diff --stat "$BASE..$FINAL"
git diff "$BASE..$FINAL"
```

审查报告必须记录 `BASE` 和实际 `FINAL` SHA。只审查上述已提交范围；忽略工作树中不属于该范围的 Compose、历史计划删除、`platform.drawio`、未跟踪审查文档和用户未提交的对象池容量调整。

权威规格与使用文档：

- `docs/superpowers/plans/2026-07-13-routerinfo-runtime-refactor.md`
- `docs/superpowers/specs/2026-07-13-routerinfo-cache-event-websocket-design.md`
- `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`

## 必查内容

1. RouterInfo 是否只保留 ServiceContext 内长期路由元数据，冻结后的公开字段篡改是否在查询、枚举和执行边界 fail closed；`TempStore` 是否仅作兼容废弃字段且框架内部无请求级状态写入。
2. IRouter 对象池的创建、Reset、Parse/Validation/Do、Clean 和归还是否闭环；panic、观察快照、自定义 Factory/Reset/Clean 是否不会泄漏请求状态。
3. ServiceContext registry 是否对同名不同配置 fail closed，关闭期间不返回 terminated 实例，多 waiter 是否只重建一次，关闭后是否精确注销。
4. ServiceEventBridge 是否每服务独立；观察事件 best-effort、无订阅者直接丢弃；控制事件是否按 shard 串行、不静默丢弃，入队超时是否不入队、返回稳定错误并精确计数。检查关闭、context 取消、worker 阻塞和 goroutine 泄漏。
5. RouteWebSocketHub 是否 ServiceContext 级隔离；完整 hash 是否为隔离边界；每客户 Router 租约、重复订阅附加租约、canonical Router、注册失败、最后退订和 Hub Close 是否只释放一次。特别检查激活中并发注册/关闭交错、回调顺序和对象池引用。
6. RouteCacheManager 的 L1/L2/L3 值类型、有效 TTL 共享与 ±10% jitter、Redis 权威 generation、SETNX 冷键、Enable/Delete/Recover 并发单调性是否正确。Recover 是否不会重建已删除路由、不会命中旧世代；共享模式失效发布错误是否继续 fail closed/degraded。
7. 是否存在数据竞争、死锁、无界队列、双重释放、超时后迟到副作用、跨 ServiceContext 全局可变状态、敏感日志或公共 API/JSON/配置兼容性破坏。
8. 测试是否确定性制造交错，修复前能失败，严格断言权威值、miss、释放次数和错误语义，而不是依赖 sleep/retry 刷绿。
9. 计划、设计和使用指南是否与实现一致，提交 SHA 是否可解析，是否有任务被错误标记为已外部 APPROVED。

## 建议验证命令

```bash
GOCACHE=/private/tmp/core-codex-go-cache go test ./pkg/server/... -count=1
GOCACHE=/private/tmp/core-codex-go-cache go test -race ./pkg/server/types ./pkg/server/event ./pkg/server/router ./pkg/server/routecache -count=1
GOCACHE=/private/tmp/core-codex-go-cache ./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-go-cache ./scripts/test.sh release-contract
GOCACHE=/private/tmp/core-codex-go-cache ./scripts/ci.sh required/quick
GOCACHE=/private/tmp/core-codex-go-cache ./scripts/ci.sh required/contracts
```

若沙箱禁止 loopback 端口，请在允许 `httptest`/REST 监听临时本机端口的环境重跑，不要把 `bind: operation not permitted` 归类为代码失败。不要启动 Redis/NATS/Docker，除非审查者显式决定扩展验证。

## 输出格式

1. `Findings`：按 P0/P1/P2 排序，每项给出文件、行号、可复现场景、影响与最小修复建议。
2. `分项结论`：分别评估 RouterInfo/对象池、ServiceContext、EventBridge、WebSocket、RouteCache、兼容性和文档台账。
3. `测试证据`：列出实际执行命令和结果，明确未运行的项。
4. `残余风险`：只列真实未关闭风险，不得把未配置的可选外部依赖写成失败。
5. `最终裁定`：只能是 `APPROVED` 或 `CHANGES_REQUIRED`。任何 P0/P1 存活时必须 `CHANGES_REQUIRED`；无 P0/P1 时可 `APPROVED` 并单独登记 P2。

请保持只读，不要自动修复，不要创建提交，不要推送。
