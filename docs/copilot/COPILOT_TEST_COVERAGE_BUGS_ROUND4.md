# Copilot Test Coverage Review - Round 4

Review target: `4946eb2 test(round3): fix CRUD panic, complete MQ integration tests, add WayPlus tests`

结论：Round 3 解决了部分核心问题，尤其是 `ClusterSwitchProvider` 已补上 API begin -> complete / rollback 的成功路径，Go 单元测试当前通过。但仍有几个测试覆盖和可验证性缺口，Copilot 的“all CRUD happy-path”和“MQ/event-stream 集成已补齐”结论还不能成立。

## 已验证通过

- `go test ./pkg/server/router ./pkg/server/api/manage ./service/manage ./pkg/server/mq ./pkg/server/event`
  - 通过。
- `go test -tags=integration -v ./tests/integration/...`
  - 通过，但 Redis/NATS/etcd/Consul 相关测试在当前环境均因环境变量未设置而 `SKIP`。
- `gofmt -l pkg/server/router/servicecontext_test.go pkg/server/api/manage/clustermanage_test.go service/manage/crud_test.go tests/integration/mq_provider_test.go`
  - 无输出，新增 Go 测试格式已正确。

## P1 - service/manage 仍未覆盖全部 CRUD happy path

文件：`service/manage/crud_test.go`

当前新增测试覆盖了：

- View schema 生成
- Add happy path + DoBefore/DoAfter
- SearchBefore stop/error
- Edit DoBefore stop/error
- Remove happy path + DoBefore stop/error
- Submit DoBefore stop
- Release update queue + DoBefore stop/error

但缺少真正的 happy path：

- `Search.Do` 调用 `LoadList` 后返回 `TableData`，并触发 `SearchAfter`。
- `Edit.Validation` 成功加载旧数据，`Edit.Do` 合并字段、调用 `Update` + `Save`，并触发 `DoAfter`。
- `Submit.Do` 成功 `SearchId` 到 `BaseModel`，将 `State` 从 `0` 改为 `1`，并调用 `Update` + `Save` / `DoAfter`。

要求：

- 补齐上述 3 个成功路径测试。
- mock adapter 需要记录 `Load`、`Update`、`Commit` / `Save` 相关效果，不要只检查 hook 分支。
- 测试名建议：
  - `TestSearch_HappyPath_LoadsListAndCallsSearchAfter`
  - `TestEdit_HappyPath_UpdatesOldItemAndSaves`
  - `TestSubmit_HappyPath_SetsStateAndSaves`

## P1 - MQ/event-stream 仍未形成框架级集成测试

文件：

- `pkg/server/event/stream.go`
- `pkg/server/mq/factory.go`
- `tests/integration/mq_provider_test.go`

当前 `tests/integration/mq_provider_test.go` 只验证：

- Redis Streams / NATS JetStream provider 的 publish -> subscribe -> ack -> health contract。
- 手写 CloudEvents-like envelope 可以通过 MQ provider 往返。

但 `pkg/server/event.Stream` 仍是本地内存 bus，没有通过 `MQManager` 发布/订阅；测试也没有验证“ServiceContext 初始化 MQManager 后，event-stream 使用该 MQ provider”的框架路径。

要求：

- 增加一个框架级的 MQ-backed event stream 适配层或测试缝，使 `event.Envelope` 能通过 `MQManager.Publish/Subscribe` 往返。
- 增加无需外部 Redis/NATS 的单元测试，使用 fake `mq.MQProvider` 验证：
  - `event.Envelope` 序列化为 MQ message。
  - MQ message 反序列化后投递给 event handler。
  - ack/handler error 的行为符合当前设计，若暂未支持错误返回，需要在测试中固定当前语义。
- 保留外部 Redis/NATS integration 测试，但不要把环境变量跳过的测试当作已验证通过。

## P1 - WayPlus 测试文件存在但不可运行，前端测试链路未完成

文件：

- `web/admin/package.json`
- `web/admin/src/components/WayPlus/__tests__/WayPage.test.tsx`
- `web/admin/src/components/WayPlus/__tests__/WayTable.test.tsx`
- `web/admin/src/components/WayPlus/__tests__/WayForm.test.tsx`

当前状态：

- `web/admin` 没有 `node_modules`，也没有 lockfile。
- `npm test -- --runInBand src/components/WayPlus/__tests__/WayPage.test.tsx src/components/WayPlus/__tests__/WayTable.test.tsx src/components/WayPlus/__tests__/WayForm.test.tsx` 失败：`sh: jest: command not found`。
- `package.json` 中 `antd` 是 `^5.29.1`，Copilot 报告称该版本安装失败；无论根因是否为版本发布问题，当前前端测试未被实际验证。

要求：

- 让 WayPlus 测试能在干净 checkout 中安装并运行：
  - 提供可复现的 lockfile，或调整依赖到当前可安装组合。
  - 运行并报告 `npm test -- --runInBand src/components/WayPlus/__tests__/WayPage.test.tsx src/components/WayPlus/__tests__/WayTable.test.tsx src/components/WayPlus/__tests__/WayForm.test.tsx`。
- 如果依赖暂时无法安装，需要把原因固定到文档或 issue，并不要宣称 WayPlus 测试已通过。

## P2 - ServiceContext 还缺 `Transport.Internal=mq` 的真实 panic 测试

文件：`pkg/server/router/servicecontext_test.go`

当前已补：

- `Cluster.Mode=on` 真实 `NewServiceContextWithConfig` panic。
- `Transport.Internal=quic` 真实 panic。
- `MQ.Mode=on` provider 不可达真实 panic。

缺口：

- `Transport.Internal=mq` 在 `transport.BuildSelector` 单测里会报错，但还没有经过 `NewServiceContextWithConfig` 的真实 panic 测试。

要求：

- 增加 `TestNewServiceContextWithConfig_TransportMQ_Panics`。
- 断言 panic message 或 recover 内容中包含 `mq` / `not implemented`，避免只测“任意 panic”。

## 提交要求

- 修复后提交一次 commit，并在回复中提供 commit hash。
- 回报必须包含：
  - Go 测试命令和结果。
  - integration verbose 输出中 Redis/NATS 是否执行或跳过。
  - WayPlus Jest 测试命令和结果。
  - 若某项因外部服务或依赖不可用无法执行，需要明确标成“未验证”，不要写成通过。
