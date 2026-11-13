# ReliableWriteStore 有界本地分页读取

## 公开入口与旧行为

v1.12.0 加性提供：

```go
func (store *ReliableWriteStore[T]) ScanLocalPage(
    ctx context.Context, prefix, after string, limit int,
) (items []*T, nextCursor string, err error)
```

旧 `ScanLocal(ctx, LocalScanOptions{Prefix: ..., Limit: ...})` 不变：非正 Limit 仍全量返回，正数只取首段，不能循环该方法完成分页。`LocalScanOptions` 不加字段，原无字段名构造也兼容。不升级 Badger，不改写入器、配置、StoreID、policy、顺序、版本、同步后清理或存储格式。

底层原 `PrefixedBadgerDB.ScanPage` 保留原签名与行为；新门面复用现有 Badger 只读事务，单独补齐范围、预算、错误和取消合同，不能原样转发旧分页的物理 LastKey。业务不得反射私有 db、另开同卷或借 ForceSync/PurgeLocal 遍历。

## 用法

```go
cursor := ""
for {
    page, next, err := store.ScanLocalPage(ctx, "record/", cursor, 128)
    if err != nil {
        return err // 保留原 cursor，不把错误当作恢复完成
    }
    for _, record := range page {
        if err := foldRequiredState(record); err != nil {
            return err
        }
    }
    // 此处释放本页。只有必要的恢复状态留存；不要把全部原件重新追加进总 slice。
    if next == "" {
        break
    }
    cursor = next // 空 page 也必须继续，只有空 next 才表示本范围结束
}
```

`store` 是原实例持有的 `ReliableWriteStore[T]`，不增加 worker 或第二套 store。应用自己协调恢复时的准入、有效 owner/fence 和后续重放。上述 `foldRequiredState` 表达应用既有恢复逻辑，Core 不猜业务索引或事实是否可以删除。

## 固定预算与结果

| 预算 | 公开常量 | 数值及含义 |
| --- | --- | --- |
| 请求返回条数 | `LocalScanPageMaxItems` | limit 必须为 1–1000，非法明确报错，不截断修正、不将 0 当全量 |
| 每页访问键数 | `LocalScanPageMaxVisitedKeys` | 最多4096个当前可见物理键，包含编码 tombstone；不计 Badger 内部跳过的旧版本/删除历史 |
| 每页原始字节 | `LocalScanPageMaxBytes` | 最多8MiB，为物理键长度与 wrapper 编码长度的累计；禁用值预取，读取前检查单项大小 |

范围在 `IteratorOptions.Prefix` 中精确限定；只取最新可见版本，逻辑 tombstone 不返回。任一预算先到即续页，返回项数可少于 limit；删除记录满窗口可返回空 items 和非空 nextCursor。游标按最后实际访问键推进，不只跟最后返回模型推进。为判断范围是否结束，迭代器会查看后继键的元数据，不额外读取其 value。

8MiB 不是 Go 堆上限：JSON 解码、模型 NewModel、业务折叠、底层块缓存、MVCC 查找等各有内存或工作量。单项编码大于预算返回 `ErrLocalScanItemTooLarge`，不跳过、确认或删除它；不能通过下一页掩盖该原件。该 API 不改变现有写入允许的大小，也不承诺单次底层 Seek 的绝对 CPU/时间上限。

## 游标、错误及生命周期

- after 为空开始；nextCursor 为空才结束。游标是不透明的版本化编码，绑定绝对 store 路径、模型前缀和请求 prefix。同原路径/模型/前缀的新进程可以继续使用；路径迁移后从头按应用幂等恢复，不手改游标。
- 游标不是权限、签名或加密凭证；只供受信应用原样传回，不记录它或把它当外部用户授权依据。格式/版本/范围不符返回 `ErrInvalidLocalScanPage`。
- 读取/解码失败及非删除记录缺少 Item 返回 `ErrLocalScanData`（底层错误保留 cause）；View 等存储错误保持原链。整页出错返回 nil items、空 cursor，不静默跳过坏记录，也不泄漏半页可继续位置。
- nil ctx 沿旧门面习惯视为 Background。循环内检查 ctx 与 store 关闭状态，结束前再次检查；取消/关闭不返回成功页。不会启动扫描 goroutine，不能强制中断正在执行的底层 I/O 或任意模型 hook。
- 每页只读，不绑定 write-behind 目标也可读取；不调用 target、失败处理或 IsSyncAfterDelete，不改 pending、attempt、组首、索引、历史与确认计数。

## 一致性、恢复与回退

每页是独立 `db.View`，**没有跨页一致快照**。页间更新的后方 key 会读到其新值，前方新插入或更新不保证重读；移除上一页末项后仍按排他游标续读。应用必须沿用自己的恢复准入、水位/fence/重放协议，不能把分页完整结束当作并发恢复业务完整性的证明。

分页限制一次材料化，但应用若把所有页的完整 Envelope/模型长期存入 map/slice，最终堆仍随总量增长。应边读边折叠确有必要的状态，释放页；不建议用清 pending、历史、租约或 TTL 解决恢复内存。

新方法不写新状态，原卷可按既有同代状态合同打开，游标本身不是卷格式。新增调用不能编译到不含该 API 的旧模块；应用若回退需同时撤销新增调用。既有终态保留/有界失败/ownership 的版本回退限制仍有效，不能由本只读 API 推导任意旧版均可打开原卷。

## 验证边界

Core 使用真实临时 Badger、SyncWrites=true、DetectConflicts=true，覆盖分页/跨范围/预算/删除/损坏/取消/关闭及新 PID 原目录恢复；具体命令、RED、差轮和测量见[本轮验收](RELIABLE_LOCAL_SCAN_PAGE_2026-11-13.md)。没有连接 Bitzoom、Full-E4、业务 SQL 或 Broker，不宣称消费方 OOM、业务功能或容量已通过。
