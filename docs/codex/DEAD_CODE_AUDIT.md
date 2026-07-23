# 无用与未完成代码审计

## 结论

本轮清理遵循“先证明可达性，再删除或显式拒绝”的原则。已删除纯注释旧实现，修复运行时占位成功和敏感输出，并将重复 SQLite 实例所有权合并到单一并发安全注册表。对仍属于公共 Go API 的入口不做无迁移窗口删除。

## 清理台账

| 候选项 | 调用面证据 | 最终分类 | 处理结果 |
| --- | --- | --- | --- |
| `pkg/persistence/adapter/cache.go` | 仓库内无活跃调用；仅 `safe/verify.go` 注释提及，但类型已导出 | `unsupported` | 保留兼容入口；所有公共方法返回可由 `errors.Is` 识别的 `ErrCacheAdapterUnavailable`，不再 nil 解引用。Redis KV 替换按 go-zero 审计另立迁移切片 |
| `pkg/persistence/adapter/nosql.go` | 文件全部为注释，仓库无活跃符号引用 | `remove` | 已删除；历史实现由 Git 保存 |
| `pkg/persistence/database/nosql/mongo.go` | 仅 Mongo 集成测试显式构造；类型属于导出 API | `unsupported/keep-domain` | 事务、Raw、Exec、统计查询返回显式错误；补齐 Rollback；Commit 不再 panic；连接 Ping 失败向调用方返回并清理客户端 |
| SQLite 全局注册表 | `adapter` 和 `entity` 都会按逻辑库名取实例 | `merge` | owner 移至 `database/oltp` 的 `sync.Map`；原有两个导出函数保留为兼容包装；并发测试证明跨包返回同一指针 |
| `pkg/server/safe/twosteps/google.go` | `VerifyCode` 为活跃导出方法；文件尾部 demo 无调用方 | `remove-debug` | 删除 stdout 验证码输出、包内 demo `main/initAuth` 和全局错误变量；行为测试捕获 stdout 并要求为空 |
| `pkg/server/trans/quic` | 配置校验明确拒绝 QUIC；`run/server.go` 注册代码已注释；包仍导出 `Server/NewServer` | `unsupported/deprecate` | 本轮不删除公共包。配置路径保持 fail-fast；直接导入面的日志、panic 和正式废弃登记转任务 8/9 |
| `pkg`、`service` 中运行时控制台和异常日志 | 仍存在多处标准输出、标准日志和非结构化 `logx` | `replace` | 转任务 8，以静态门禁和逐包测试统一处理 |
| `pkg/utils/eventbus.go` | 全仓无生产或示例调用；自 2022 年首次提交后无演进；现行事件入口位于 `pkg/server/event` | `remove-experimental` | 删除 `utils.Publisher`；进程内事件使用 `event.Stream`，服务事件使用 `ServiceContext` 管理的 EventBridge |

## 已验证契约

- 缓存未配置时，Get/Set/Del/Scan/Search 均返回显式错误，不 panic。
- TOTP 校验不向 stdout 输出密钥、动态码或比较结果。
- Mongo 未支持操作不会返回虚假成功，也不会以占位 panic 终止进程。
- 相同逻辑数据库名在 adapter/entity 以及并发调用下复用同一 SQLite 实例。
- QUIC 配置仍在启动前被拒绝，未伪装为可用传输。

## 验证命令

```bash
go test ./pkg/persistence/adapter ./pkg/persistence/database/nosql ./pkg/server/safe/twosteps -count=1
go test -race ./pkg/persistence/entity ./pkg/persistence/adapter -count=1
go list ./...
rg -n 'panic\("implement me"\)|mongo implement|TODO implement me' pkg/persistence --glob '*.go'
rg -n 'fmt\.(Print|Printf|Println).*secret|fmt\.(Print|Printf|Println).*code' pkg/server/safe --glob '*.go' -i
rg -n 'globalSqliteInstances' pkg --glob '*.go'
```

最后三条扫描应无结果。
