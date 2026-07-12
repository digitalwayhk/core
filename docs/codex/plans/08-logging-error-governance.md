# 任务 8：日志与异常治理实施计划

## 目标

统一生产库日志到 go-zero `logx`，删除 stdout、标准库 logger、进程终止日志、装饰性文案和敏感数据 dump。错误由拥有重试、降级、响应或终止决策的边界记录一次；下层只包装并返回。

## 范围与批次

- [x] 8.1 建立静态守卫、日志规范和当前清单，并接入 `quick`。
- [x] 8.2 服务边界：`router`、`run`、`trans/rest`、`trans/socket`、`safe`、`utils`。
- [x] 8.3 基础设施：`cluster`、`mq`、`event`、`transport`，校正 retry/fallback/final failure 级别。
- [x] 8.4 持久化：移除原始 SQL、对象、配置和记录值，保留连接/恢复/迁移/最终失败事件。
- [x] 8.5 WebSocket/通知：worker 生命周期降为 debug，丢弃、panic、关闭超时保留 error。
- [x] 8.6 总验收：静态守卫、vet、定向测试、race 和 JSON 日志契约测试。

## 规范

- 事件名使用 ASCII `snake_case`，放在日志第一参数。
- 字段使用 `logx.Field`；优先字段为 `service`、`trace_id`、`route`、`method`、`operation`、`provider`、`node_id`、`attempt`、`duration_ms`、`error`。
- `Errorw` 仅用于最终失败、数据损失风险、panic 恢复或没有成功回退的依赖失败。
- `Infow` 用于服务生命周期、Provider 切换、成功恢复和已处理降级。
- `Debugw` 用于每次重试、路由注册、worker 生命周期和高频诊断。
- 不记录 token、密码、cookie、TOTP、DSN、完整 payload/body/response、带值 SQL 或对象 dump。
- 可复用库不得 `Fatal`、`Panic` 或写 stdout；启动失败必须通过 error 或受控生命周期边界传播。

## 验收命令

```bash
./scripts/check-logging.sh
./scripts/test.sh quick
go vet ./pkg/server/... ./pkg/persistence/... ./service/...
go test ./pkg/server/... ./pkg/persistence/... ./pkg/utils ./service/manage/... -count=1
go test -race ./pkg/server/router ./pkg/server/cluster ./pkg/server/mq ./pkg/server/types -count=1
```

需要监听端口的测试在受限沙箱外运行；其余测试不得依赖 Docker 或网络。
