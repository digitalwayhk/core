# Core 自动验证计划

## 基础验证

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex
rtk go test ./...
rtk go test -short ./...
rtk go test ./pkg/server/...
rtk go test ./pkg/persistence/...
rtk go test ./service/manage/...
```

## 集成验证

```bash
CORE_TEST_CLUSTER_LOCAL=1 rtk go test -tags=integration ./tests/integration/... -run TestClusterLocal
CORE_TEST_REDIS_STREAM=1 CORE_TEST_NATS=1 rtk go test -tags=integration ./tests/integration/... -run TestMQ
CORE_TEST_ETCD=1 rtk go test -tags=integration ./tests/integration/... -run TestClusterEtcd
CORE_TEST_CONSUL=1 rtk go test -tags=integration ./tests/integration/... -run TestClusterConsul
```

## 示例 Smoke

```bash
rtk go run ./examples/01-hello-router/main -p 18081
rtk go run ./examples/demo/main -p 18080
```

## WebSocket / Notice P0 验证

```bash
CORE_TEST_CLUSTER_LOCAL=1 rtk go test -tags=integration ./tests/integration/... -run 'Test.*WebSocket|TestCrossNode'
```

通过标准：

- 多节点 WebSocket/Notice 集成测试通过。
- 私有订阅不串用户，hash 推送不串 market/user/symbol。
- 节点下线或重连场景不漏推、不重复推。

