# 任务 9：架构加固实施计划

## 目标

复核总审查中的共享状态、跨节点转发、配置迁移和大型持久化模块问题。已由任务 12 关闭的项目只记录当前证据；仍存在的问题采用独立测试和提交修复；高风险结构拆分建立可执行边界，不与行为修复混合。

## 执行清单

- [x] 验证 ServiceContext 全局状态已由 `serviceContextRegistry` 统一持有并通过互斥锁保护。
- [x] 验证 CrossNode forwarder 已按服务名键控；旧全局入口仅作带 Deprecated 注释的兼容回退。
- [x] 使用 `net.JoinHostPort` 构造跨节点目标，支持 IPv6，并将非 2xx 响应作为错误返回。
- [x] 让配置迁移返回读、解析、编码和写入错误；`ReadConfig` 在责任边界记录 `config_migration_failed`。
- [x] 为 3257 行 SharedBadger 模块登记分阶段拆分边界、依赖顺序和验收门禁，不在本任务中做无行为收益的大规模搬文件。
- [x] 运行 config/cluster/router/types 定向测试与 race。

## 验收命令

```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/router ./pkg/server/types -count=1
go test -race ./pkg/server/cluster ./pkg/server/router ./pkg/server/types -count=1
./scripts/check-logging.sh
```
