# `pkg/utils` 高风险 API 审计

## 结论

`pkg/utils` 文件重排后的主要剩余风险不是文件布局，而是几个已导出且可能影响持久数据、分布式 ID 或密文的历史 API。本轮不原地改变这些行为，避免在“整理 utils”中隐式改变业务唯一性、幂等键、节点 ID 或密文格式。

优先级最高的是 `NewAlgorithmSnowFlake`：当前用十进制文本直接拼接 `DataCenterID` 和 `MachineID`，会产生确定性 worker 冲突，而且可把超出下游 6 位限制的值传入发号器。该问题应单独修复并做多节点 ID 不重复验证。

## 范围和分类方法

- 对所有高风险候选入口执行全仓 Go 调用面扫描。
- 区分“无仓内调用”和“可直接删除”：已导出 API 仍可能有外部消费方。
- 对影响持久化哈希、幂等键、分布式 ID 和密文的问题，本轮只登记迁移方案。
- 对会 panic、吞错或依赖进程全局状态的兼容 API，优先新增返回 `error` 的替代入口，再给旧入口安排废弃窗口。

## 风险台账

| 级别 | API | 当前证据 | 本轮决策 | 后续动作 |
| --- | --- | --- | --- | --- |
| 高 | `NewAlgorithmSnowFlake` | 全仓 8 个 Go 文件涉及该符号；`(machine=23, dc=1)` 和 `(machine=3, dc=12)` 都映射到 worker `123` | 保留旧行为 | 单独设计显式位分配，在多节点、回拨和重启场景验证 ID 不重复 |
| 高 | `HashCodes` | 定义在内共 52 个 Go 文件调用；`("a-", "b")` 和 `("a", "-b")` 都先组合成 `a--b-` | 保留旧行为 | 新增长度前缀或结构化编码的 `HashCodesV2`，按存储、缓存和幂等场景分批迁移 |
| 高 | DES/3DES 及 padding API | 仓内只有 `legacy_crypto.go` 定义；忽略 cipher 创建错误，非法 key、非整块密文或非法 padding 可 panic；算法和固定 IV 不适合新业务 | 保留已导出兼容面 | 新建返回 `error` 的解密迁移工具，确认外部消费方和历史密文后登记废弃 |
| 中 | `GetRandNum` | 仓内只有定义；`n <= 0` 会 panic；调用已废弃的全局 `rand.Seed`，且在 Go 1.24 起该函数为 no-op | 保留已导出兼容面 | 新增有边界错误的随机 API，或在确认外部无调用后废弃 |
| 中 | `ToTime` | 仓内只有定义；吞掉 `ParseInt` 错误，非法值被当作 Unix epoch | 保留已导出兼容面 | 新增 `(time.Time, error)` 入口，迁移后废弃旧函数 |
| 中 | `IsExista` | 配置路径和 SQLite 测试仍在使用；权限等 `Lstat` 错误也会被解释为“存在” | 保留行为 | 用 `(bool, error)` 替代入口区分不存在与检查失败 |
| 中 | `CreateDir` | 配置初始化仍在使用；强制 `0777` 并二次 `Chmod` | 保留行为 | 让调用方提供权限，新默认使用最小权限并补权限失败测试 |
| 中 | `GetOutBoundIP` | 仓内只有定义；依赖 `8.8.8.8:53` 路由可达，并直接断言 `*net.UDPAddr` | 保留已导出兼容面 | 让目标可注入，安全处理地址类型；启动逻辑不应依赖外网 DNS |
| 低 | `PrintObj` | 一个活跃调用用于构造错误文本；序列化错误被吞掉 | 保留行为 | 调用方避免向日志或响应写入完整业务对象，需调试信息时使用受控字段 |

## Snowflake 兼容风险

`NewAlgorithmSnowFlake(machineId, dataCenterId)` 当前执行以下映射：

```text
worker = uint16(parseDecimal(decimal(dataCenterId) + decimal(machineId)))
```

这不是可逆的二元组编码。除了 `1/23` 与 `12/3` 都得到 `123`，默认全局请求 worker 还使用 `(1000, 1000)`，十进制组合值 `10001000` 转为 `uint16` 后发生截断。

当前依赖 `idgenerator-go v1.3.3` 的默认 `WorkerIdBitLength` 为 6，因此文档化上限是 63。`SnowWorkerM1` 构造路径没有对 `WorkerId` 做上限校验，而是直接将它左移到序列号之上；超界位可与时间区域重叠。

修复时不能只将字符串拼接换成另一个算术式。应先明确 `DataCenterID` 和 `MachineID` 各自位宽，再同步收紧 `ClusterConfig.Validate`、自动租约分配上限和固定 ID 模式，最后用多进程测试验证同一时间窗内不冲突。

## `HashCodes` 兼容风险

`HashCodes` 对每个参数写入原文和 `-`，没有长度或转义信息。这使不同参数列表可在进入 SHA-256 前就变成同一原文；这不是哈希算法本身的碰撞。

由于该函数已广泛用于模型哈希、缓存、Inbox/Outbox 和幂等逻辑，直接改编码会让新旧节点对同一业务键计算出不同结果。后续应使用新函数名，先支持双读/双校验，再分场景切换写入。

## 旧加密 API 兼容风险

`EncyptogDES`、`DecrptogDES`、`Encyptog3DES`、`Decrptog3DES` 及 padding 函数在仓内没有活跃调用，但它们是公共导出 API。它们还有三类必须在迁移工具中显式处理的问题：

- DES/3DES cipher 创建错误被忽略，非法 key 会在后续解引用处 panic。
- CBC 解密对非整块密文会 panic，`UnPaddingText` 对空输入或非法 padding 会越界。
- DES 使用固定 IV，3DES 直接从 key 取 IV，都不应再用于新数据。

下一步应先确认外部消费方和历史密文样本，再提供“只用于旧数据解密”的受控 API。新数据应使用现有 AES-GCM 路径。

## 删除边界

仓内无调用的 `GetRandNum`、`ToTime`、`GetOutBoundIP` 和旧加密函数不能像已删除的 `utils.Publisher` 一样立即删除：EventBus 有明确的现行替代路径，且自 2022 年后无演进；其他函数仍可能被模块外部导入。对这些 API 应先查消费方矩阵，新增替代入口并登记废弃窗口，再在允许破坏性变更的版本删除。

## 复现命令

```bash
rg -l 'NewAlgorithmSnowFlake' --glob '*.go'
rg -l 'HashCodes\(' --glob '*.go'
rg -l 'EncyptogDES|DecrptogDES|Encyptog3DES|Decrptog3DES|PaddingText|UnPaddingText' --glob '*.go'
rg -l 'GetRandNum\(|ToTime\(|GetOutBoundIP\(' --glob '*.go'
go test ./pkg/utils -count=1
go test -race ./pkg/utils -count=1
```
