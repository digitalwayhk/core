# 如何整理 `pkg/utils` 并修复低风险缺陷

本文定义 `pkg/utils` 的整理、审计和验证边界。实现保持 `github.com/digitalwayhk/core/pkg/utils` 包路径不变，删除已被现行事件系统替代的旧 EventBus，并只修复有回归测试保护且不需要数据迁移的问题。

## 背景与目标

`pkg/utils` 当前有 13 个 Go 文件。`types.go` 包含 700 余行代码，同时承担反射类型识别、字段访问、结构遍历、自动映射、值转换、运算和零拷贝转换。`common.go` 同时承担序列化、随机数、时间、哈希、字符串和格式校验。文件边界无法直接表达能力边界。

本次整理实现以下目标：

1. 保持现有包路径和常用导出函数签名，按职责重排源文件
2. 删除全仓零调用且已被 `pkg/server/event` 替代的旧 EventBus
3. 使用测试复现并修复并发、Unicode、安全转换和反射生命周期问题
4. 审计可能影响历史密文、持久化哈希和分布式 ID 的高风险实现
5. 为每个文件和导出 API 补充中文边界注释

## 不在本次范围内的工作

本次整理不修改下列内容：

- 不拆分新的 Go 子包，不修改调用方的 `utils.Xxx` 写法
- 不升级 OpenTelemetry、ClickHouse、kin-openapi 或其他依赖
- 不改变已落库哈希、密文格式或 Snowflake ID 规则
- 不使用一次性大规模重写替换全部反射辅助函数
- 不修改 `pkg/server/event` 的 Stream、EventBridge、Outbox 或外部消息队列语义

依赖漏洞升级作为后续任务处理。OpenTelemetry、ClickHouse 和 kin-openapi 分别使用独立提交和验证范围。

## 方案选择

### 采用方案：保持包路径并按职责拆文件

该方案保留源码调用方式，只重排同一包内的实现。低风险缺陷通过测试驱动开发修复，高风险问题先登记证据和迁移影响。

### 未采用方案：只移动文件

只移动文件可以降低行为风险，但会保留已经确认的 panic、Unicode 损坏、无所有者 goroutine 和 `unsafe` 别名问题，无法完成缺陷检查目标。

### 未采用方案：拆分多个子包

拆分 `utils/reflectx`、`utils/cryptox` 等子包会修改大量导入和调用。当前收益不足以承担迁移、公共 API 和消费方验证成本。

## 文件组织

所有文件继续声明 `package utils`。实现阶段根据实际依赖按以下能力分组，避免为了文件数量机械拆分：

| 文件组 | 主要职责 |
| --- | --- |
| `hash.go` | MD5、SHA-256、用户标识和组合哈希 |
| `string.go` | Unicode 首字符转换和 rune 辅助 |
| `validation.go` | 邮箱、手机号和数字文本判断 |
| `runtime.go` | 测试环境和运行时路径判断 |
| `filesystem.go` | 文件、目录、读取和删除 |
| `network.go` | 本机地址、可信代理客户端地址和端口探测 |
| `aes.go` | AES-GCM 加密和解密 |
| `key_derivation.go` | PBKDF2、盐值和 JWT 密钥派生 |
| `legacy_crypto.go` | DES、3DES 和旧填充兼容 API |
| `reflection_type.go` | 类型识别和实例创建 |
| `reflection_field.go` | 字段查询、读取、写入和遍历 |
| `automap.go` | 结构映射和映射缓存 |
| `conversion.go` | 值转换、数值运算和字节字符串转换 |
| `concurrency.go` | 有界并发、保序结果、取消和 panic 转换 |
| `number.go` | 数字文本判断，修正原文件名 `nubmer.go` |
| `snowflake.go` | Snowflake worker 构造 |

文件名可以在实现时合并，但每个文件只能承担一个紧密相关的能力组。测试文件跟随被测能力拆分。

## EventBus 删除设计

`pkg/utils/eventbus.go` 只在文件内部引用 `Publisher`、`NewPublisher` 和订阅方法。全仓代码、示例和现行文档没有调用这些符号。该文件自 2022 年首次提交后没有功能演进。

现行进程内事件入口位于 `pkg/server/event`。Stream 负责进程内分发，ServiceEventBridge 负责本地事件和外部可靠事件的统一边界。保留第二套无上下文、无错误返回、无可靠投递语义的 `utils.Publisher` 会误导新调用方。

实现删除 `eventbus.go`，并在 `CHANGELOG.md` 的 Unreleased 段说明：

- 删除实验性 `utils.Publisher` 和 `utils.NewPublisher`
- 进程内事件迁移到 `pkg/server/event.Stream`
- 服务事件迁移到 `ServiceContext` 管理的 EventBridge

删除前后都运行全仓符号扫描。若实现阶段发现新的仓库内调用，则停止删除并重新评估迁移方式。

## 低风险缺陷修复

### 统一 `ConcurrencyTasks` 行为

当前 `Concurrency=1` 走 `extFun`，panic 会逃逸；并发模式走 `doFun`，panic 会写入对应结果。相同任务只因并发度不同就产生不同错误语义。

实现使用一个共享执行函数处理成功、普通错误和 panic。串行与并发模式都把 panic 转为包含 panic 值的 error，并保留结果顺序。

并发调度改为固定数量 worker，避免为每个参数创建一个 goroutine。`Concurrency<=0` 使用现有默认值，实际 worker 数不超过参数数量。

`Ctx` 为 nil 时使用 `context.Background()`。调度前或调度中发现取消后，不再启动新任务；尚未执行的位置写入 `ctx.Err()`。已经开始的 `Func` 无法被框架强制中断，调用方需要在函数内部响应上下文。

### 修复 Unicode 首字符转换

`FirstUpper` 和 `FirstLower` 当前使用 `s[:1]`，会切开多字节 UTF-8 字符。实现按第一个 rune 分割字符串，再调用 Unicode 大小写转换。空字符串继续返回空字符串。

### 移除字节与字符串的 `unsafe` 别名

`String2Bytes` 和 `Bytes2String` 当前共享底层内存。调用方修改输入切片后，已返回字符串可能发生变化，违反字符串不可变预期。

实现改用标准 Go 转换 `[]byte(s)` 和 `string(b)`。函数签名保持不变，结果改为独立副本。

### 移除反射缓存的隐式生命周期

`types.go` 的 `init` 会永久启动内存监控 goroutine，并在进程内存超过 200 MB 时删除缓存、调用 `runtime.GC()` 和记录日志。`pkg/utils` 无法判断整进程的内存压力，也没有 ServiceContext owner 管理该 goroutine。

实现删除自动监控、主动全局 GC 和包初始化 goroutine。映射缓存继续按现有 key 工作。`StopMemoryMonitor` 暂时保留为空操作兼容入口，并登记为 Deprecated，避免本次额外删除导出符号。

### 补齐 nil 和错误边界

反射辅助函数先使用测试覆盖 nil、非指针和不可设置字段。实现只修复可保持签名和成功语义的 panic。无法用现有返回值表达错误的入口不吞掉错误，也不返回虚假成功。

工作区当前已有一项未提交的 `IsPtr(nil)` 防护修改。实现阶段保留该修改，先补充失败测试，再核对它与其余反射边界的组合行为。

## 高风险问题审计

以下问题需要证据，但不能在本次整理中静默改变：

- DES 和 3DES 忽略无效 key 错误，旧 padding API 对损坏输入可能 panic
- `HashCodes` 使用分隔符拼接，部分参数组合可能生成同一原文
- `NewAlgorithmSnowFlake` 将 DataCenterID 和 MachineID 以十进制字符串拼接后压缩到 `uint16`
- `ToTime`、`PrintObj` 等旧 API 无法通过现有签名返回解析或序列化错误
- `GetRandNum` 修改进程级随机源状态，且 `n<=0` 会 panic

审计输出记录调用面、失败样例、兼容影响和建议迁移 API。涉及历史数据、跨节点 ID 或密文兼容的问题进入独立设计，不在文件整理提交中改变行为。

## 测试与验证

实现遵循测试驱动开发。每个行为修复先新增失败测试，确认失败原因后再修改生产代码。纯文件移动前先运行现有测试建立基线，移动后运行同一组测试证明行为未变化。

定向验证命令：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/utils -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./service/manage ./pkg/persistence/types -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -run '^$'
rtk proxy ./scripts/test.sh release-contract
```

静态检查包括：

```bash
rtk rg -n '\b(NewPublisher|Publisher|SubscribeTopic)\b' . --glob '*.go' --glob '*.md'
rtk rg -n 'unsafe' pkg/utils --glob '*.go'
rtk gofmt -d pkg/utils
```

全仓编译或发布契约若因外部数据库、网络或消费方环境失败，交付说明必须区分代码失败与环境阻塞，并保留完整命令和错误证据。

## 提交边界

本次实现拆成可独立复核的提交：

1. 添加缺陷回归测试并修复低风险行为
2. 按职责重排文件，不改变导出 API
3. 删除旧 EventBus，更新 changelog 和审计记录

现有未提交的 `pkg/utils/types.go` 修改属于工作区已有变更。提交时只纳入已确认与本任务一致的部分，不覆盖或丢弃其他修改。

## 验收标准

- `pkg/utils` 包路径和常用导出函数签名保持不变，EventBus 删除项除外
- `types.go` 和 `common.go` 不再混合多个能力域
- 串行与并发任务使用一致的 panic 和错误语义
- Unicode 首字符转换不破坏 UTF-8 文本
- 字节和字符串转换不共享可变底层内存
- 导入 `pkg/utils` 不再启动后台 goroutine 或触发全局 GC
- `eventbus.go` 删除后全仓没有旧 Publisher 引用
- 高风险问题有证据和后续建议，没有未经迁移的行为变化
- 定向测试、race、全仓编译检查和发布契约结果都有最新证据
