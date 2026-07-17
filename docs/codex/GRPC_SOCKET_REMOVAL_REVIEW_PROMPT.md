# gRPC 默认传输与 Socket 删除最终外审提示词

请对 Digitalway Core 的 gRPC 默认内部传输、mTLS、服务发现和自定义 Socket 删除做只读最终审查，不修改代码。

## 审查范围

- 工作区：`/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex`
- 起点：`6c07a03`（实施计划基线）
- 终点：执行审查时当前 `HEAD`（实现代码 tip 为 `3d6b888`，其后只允许任务 10 证据/提示词提交）
- 命令：`git diff 6c07a03..HEAD`
- 设计：`docs/superpowers/specs/2026-07-17-grpc-default-socket-removal-design.md`
- 计划：`docs/superpowers/plans/2026-07-17-grpc-default-socket-removal.md`
- 迁移：`docs/codex/GRPC_TRANSPORT_MIGRATION.md`
- MAJOR 批准：`docs/codex/BREAKING_CHANGE_APPROVAL.md` 中 `socket-to-grpc-v1`

用户已明确批准自定义 Socket 直接删除、不走长期废弃周期。不要仅以“公开 API 发生破坏”为由要求恢复兼容 shim；应检查破坏范围是否完整进入 MAJOR 批准、迁移、apidiff 和消费方门禁。

## 必查实现

1. 客户端是否只缓存/复用 go-zero `zrpc.Client`，Core `ClusterProvider + ServiceResolver` 是否仍是唯一发现权威。
2. 服务端薄 grpc-go 适配是否满足每个 ServiceContext 独立 listener、标准 `grpc_health_v1`、NOT_SERVING、GracefulStop 超时和强制 Stop。
3. `GRPCPort` 是否贯穿 Local/Redis/Consul 发现、Resolver 和 TransportEndpoints；HTTP 端口不得冒充 gRPC。
4. insecure/tls/mtls/mesh 的默认、证书、CA、动态 `{service}` ServerName 和错误身份是否 fail closed。
5. fallback 是否只发生在发送前；发送开始后不得跨协议重试。统计必须能证明 gRPC 选择、HTTP fallback、成功/失败和 inbound gRPC。
6. 示例 06 同进程使用 local+insecure，三进程使用 Redis+mTLS；三进程业务链必须证明 User -> Order -> Supplier 使用 gRPC、HTTP=0、SendFailure=0。
7. 自定义 Socket 两个包、配置、flag、NodeInfo/payload/observe/attach 字段是否删除；WebSocket 和 Unix socket 必须保留。
8. 旧 JSON 是否幂等删除 `SocketPort`、`Transport.Socket`、`Transport.GRPC.Enable` 并保留未知字段。
9. EventBridge/Observe 是否只携带逻辑服务名并经过 Resolver，不得再依赖 Socket/静态地址。
10. apidiff 不兼容项是否全部属于 `socket-to-grpc-v1` 批准：Socket 表面、Transport/Selector/Selection、SendWithFallback、CrossNodeSender、GetServers、MembershipManager、GRPC Enable。

## 已执行证据

在干净 Core `3d6b888` worktree：

```bash
go test ./pkg/server/... -count=1
go test -race ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport/... ./pkg/server/router ./pkg/server/run ./pkg/server/types -count=1
go test -p 1 -race ./examples/06-shop-microservices/... ./examples/integration/06-shop-microservices ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=20m
go vet ./pkg/server/... ./examples/06-shop-microservices/...
./scripts/check-logging.sh
./scripts/ci.sh required/quick
./scripts/ci.sh required/contracts
./scripts/ci.sh required/race
go test -race ./pkg/server/transport/grpc -run 'MTLS|WrongCA|ServerName|Health|Stop|ZRPC' -count=20
go test -race ./pkg/server/transport -run 'Fallback|Retry|Send' -count=20
```

以上最终命令均退出 0。曾观察到两类测试基础设施现象，不能隐瞒：两个重型集成包并行时 WebSocket/UAT 的 5 秒预算被争用；一次端口探测到子进程 bind 的窗口发生临时端口占用。确认无残留进程后，`-p 1` 串行两套 suite 均通过。请评估是否需要把端口 reservation 竞态列为 P2。

## 消费方证据

futures 精确提交：`e0bc32088b2125bb5d6e8880ef37c5b033541b5e`，Core 候选：`3d6b888`。

- 临时 `go.work` 指向候选；gateway API、worker 稳定测试和 services 根包编译均通过。
- Go 源码无旧 Socket API 使用。
- 13 份 `docker/local/etc/*.json` 含旧 `SocketPort`。
- 真实 `gateway.json` 临时副本加载在 Socket 迁移前因既有 `Telemetry.Batcher=jaeger` 被当前 go-zero 配置校验拒绝。
- 因此 Core 代码可以合并，但 v1.0.0 正式发布仍为 `blocked-by-consumer-verification`；不得把源码 smoke 说成完整消费方升级通过。

## 输出要求

1. Findings 按 P0/P1/P2 排序，给出文件和行号。
2. 分别裁定：Core 实现能否合并；v1.0.0 能否正式发布。
3. 审查是否存在虚假绿色、遗漏的 Socket 语义残留、未批准 apidiff 或不可靠测试。
4. 给出兼容性、安全、生命周期、服务发现、fallback 和消费方残余风险。
5. 最终输出 `APPROVED` 或 `CHANGES_REQUIRED`；若 Core 可合并但发布受阻，必须明确写成两个不同结论。
