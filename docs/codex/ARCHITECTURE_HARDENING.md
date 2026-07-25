# 架构加固台账

## 已关闭项目

| 项目 | 当前实现 | 验证 |
| --- | --- | --- |
| ServiceContext 全局注册表 | `serviceContextRegistry` 封装 map、初始化占位和快照，所有读写均受锁保护 | `servicecontext_test.go` 的缓存、并发和生命周期测试；router race |
| CrossNode forwarder 隔离 | `Set/GetCrossNodeForwarderForService` 按规范化服务名存储；旧全局方法保留为 Deprecated 兼容入口 | `crossnode_test.go` 的跨服务隔离、替换、清理和并发测试 |
| 跨节点地址与响应 | sender/HTTP 都使用 `net.JoinHostPort`；IPv6 URL 正确；HTTP 非 2xx 返回包含状态的错误 | `event_post_test.go` |
| 配置迁移写失败 | `migrateConfig` 返回完整错误链；`ReadConfig` 记录路径和错误，不再吞掉写失败 | `serverconfig_migration_error_test.go` |

## SharedBadger 拆分计划

`pkg/persistence/database/nosql/sharedbadger.go` 当前约 3257 行，同时承担本地 KV、同步队列、批量远端写入、事务回退、同步后删除、自愈、统计和生命周期。直接搬迁会给已通过的 CAS/pending/fatal-break 契约带来不必要风险，因此按以下顺序拆分，且每一步仅移动代码、不改变行为：

1. `sharedbadger_queue.go`：key 生成、pending 计数、队列扫描和状态确认。
2. `sharedbadger_batch.go`：批量 Insert/Update/Delete、事务观察器与逐条回退。
3. `sharedbadger_cleanup.go`：sync-after-delete 队列、物理删除与 coalesce worker。
4. `sharedbadger_lifecycle.go`：启动、停止、强制同步、自动限制和统计。
5. 保留 `sharedbadger.go`：类型、构造器和基础 Get/Set/Scan API。

每一步必须运行：

```bash
go test ./pkg/persistence/database/nosql -count=20 -timeout=15m
go test -race ./pkg/persistence/database/nosql -count=1 -timeout=10m
./scripts/ci.sh observational/persistence
```

禁止同时修改同步语义、重试次数、锁粒度或公共 API；发现行为需求时另立 TDD 提交。
