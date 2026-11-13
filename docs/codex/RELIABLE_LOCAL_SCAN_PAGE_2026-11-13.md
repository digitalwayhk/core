# ReliableWriteStore 有界本地读取验收（2026-11-13）

## 根因与实施边界

基线 `v1.11.3` / `709e6a1063705d2fedcb2e0fcd8fcf99cf28dc73`。`ReliableWriteStore.ScanLocal` 入口只检查一次ctx，随后 `PrefixedBadgerDB.Scan` 从前缀头部将所有匹配可见模型加入slice；Limit<=0无条数上限，反复正Limit也不能续页。底层已有ScanPage，但门面db私有，且旧分页欠缺精确底层Prefix、扫描中ctx、物理访问/字节预算和错误不跳过合同。此处是正式门面能力缺口，不把消费方自己的无界恢复读取归因owner容量刷新。

在干净的既有隔离工作区创建 `feat/reliable-local-scan-page`；原用户工作树、Pebble及其他分支不动。85个原已跟踪nosql Go文件与v1.11.3逐字一致，生产只增加 `reliable_write_local_scan.go`，不覆盖旧Scan/ScanPage或写路径。源码绑定见附件 `baseline-production-bindings.json`。

新增 `ReliableWriteStore[T].ScanLocalPage(ctx,prefix,after,limit)`，每页直接走现有Badger View：范围及游标校验 → 精确Prefix、关闭value预取 → 键数/编码字节/返回数预算 → 原wrapper解码与原NewModel → 当前页结果及排他续页游标。失败返回nil项和空游标，不确认、不删、不改pending或处置状态。没有第二个提交器、队列、worker、存储或业务模型。

limit为1–1000；每页最多4096当前可见物理键、8MiB物理键+编码wrapper。三个公开常量、三个错误及方法是加性API，按正式策略发布MINOR `v1.12.0`，不是覆盖旧tag。完整接入、错误、生命周期及快照边界见[正式指南](RELIABLE_LOCAL_SCAN_PAGE_GUIDE.md)。

## RED、失败尝试与GREEN

- `red.log`：修改生产之前，真实可靠保存3条原件、旧全量返回3条，随后公开方法能力断言 `LOCAL_SCAN_PAGE_MISSING` 失败；exit1，是实际缺能力RED，不是编译/夹具错误。
- `red-old-overlay.log`：保留同一个合同fixture，85个旧Go文件逐字绑定，只隐藏新增生产文件；再次运行真实Badger获得同一exit1。overlay/fixture保留供复跑，不修改模块缓存或旧源码。
- `green-contract.log`：相同合同在实现后exit0，3条按1条分页无重无漏，pending保持3。
- `green-targeted.log`：第一次扩展专项exit1，两个夹具缺陷：默认1MiB vlog不接受3MiB测试原件；旧非自适应模式重开后缓存pending为0，不能当持久索引。原失败日志及两个失败卷均保留。大项测试仅在自己的卷采用合法32MiB vlog/128MiB memtable，仍SyncWrites=true/DetectConflicts=true；恢复断言改为核对原持久索引，未改分页预算或放宽结果断言。这不是新增生产缺陷RED。
- `green-targeted-2.log`及`green-final-targeted.log`：真实Badger专项通过。末轮增加有界卷完成/暂停/pending只读，以及实际View屏障内前台Save提交。没有将失败尝试覆盖成PASS。

覆盖正limit/上限、空范围、错误版本/范围/路径/模型游标、相邻前缀隔离；4096条tombstone空页游标推进后取回同一原范围健康项；两项3MiB达到字节预算及超大单项错误；坏JSON/非删除空Item整页失败；取消/Close解码屏障；页间变化明确非全局快照；读事务期间并发Save仍提交。

正常关闭与真实SIGKILL均由新PID重开原目录、无需新Save续页，持久pending仍7。专项普通成功PID：seed 24507→24508，hold 24509→24510；完整最终进程PID和race父子结果在原日志。测试源中的子进程入口直接运行会SKIP，父测试确实启动并验证不同PID，不能只数入口SKIP当恢复通过。

## 实际命令与回归

所有shell均按RTK规范。以下命令都由证据runner保留完整argv、耗时、退出码及日志SHA：

```bash
rtk proxy go test ./pkg/persistence/database/nosql -run '^TestReliableLocalScanPageContract$' -count=1 -v -timeout=60s
rtk proxy go test ./pkg/persistence/database/nosql -run '^TestReliableLocalScanPage' -count=1 -v -timeout=180s
rtk proxy go test ./pkg/persistence/database/nosql -count=1 -json -timeout=300s
rtk proxy go test -race ./pkg/persistence/database/nosql -count=1 -json -timeout=300s
rtk proxy bash scripts/test.sh persistence-unit
rtk proxy go test ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -json -timeout=300s
rtk proxy go test -race ./examples/04-shop-performance/... ./examples/07-shop-order-scale/... -count=1 -json -timeout=300s
rtk proxy bash scripts/test.sh release-contract
rtk proxy bash scripts/test.sh performance-contract
rtk proxy bash scripts/test.sh web-dist-sync
rtk proxy bash scripts/check-logging.sh
```

nosql初轮普通260个顶层PASS/49 SKIP/0 FAIL；增加两项只读/并发回归后，最终persistence-unit含完整nosql通过，race为262 PASS/49 SKIP/0 FAIL。SKIP保留原原因，名称清单见 `test-counts.json`。04/07普通/race数量也在该文件；它们是组件/本地适配器回归，不代表真实远程库、HTTP/WS UAT或容量。

官方apidiff对85旧文件对应表面与新nosql基线判定仅加性方法/常量/错误，无不兼容项。nosql包首次纳入现有公共包保护清单；通过唯一官方更新入口生成新基线。脚本对14个无关包的重生成产物留在本轮证据供审查，撤回这些本轮生成变动，不混入历史API更新。

release-contract内部实际执行CI shell、api-compat、public-api、config-contract及security，均exit0。正式clean-tree `--release` 检查、main/tag、代理及附件结果由发布回执提供，未完成前不宣称已经发布。

## 同机读取对照（不等于容量）

Darwin本地非tmpfs临时目录，Go/GOMAXPROCS及真实Options在日志；SyncWrites=true、DetectConflicts=true，4096条×2048字节合成载荷，AutoSync=false，旧全量与新分页各独立卷、单进程GOMAXPROCS=1，串行三轮。fixture在读计时外。旧ScanLocal生产源码逐字不变；这是同一候选内旧全量API与新增分页API的对照，不冒充两次不同正式二进制容量认证。

```bash
rtk proxy env GOMAXPROCS=1 CORE_LOCAL_SCAN_MEASURE=legacy go test ./pkg/persistence/database/nosql -run '^TestReliableLocalScanPageMeasurement$' -count=1 -v -timeout=120s
rtk proxy env GOMAXPROCS=1 CORE_LOCAL_SCAN_MEASURE=page go test ./pkg/persistence/database/nosql -run '^TestReliableLocalScanPageMeasurement$' -count=1 -v -timeout=120s
```

| 指标 | 旧全量API | 新分页API，limit=128 |
| --- | --- | --- |
| 每轮返回总数/错误 | 4096/0 | 4096/0 |
| 每轮API调用/最大返回项 | 1/4096 | 32/128 |
| 整轮墙钟，三轮ms | 23.735 / 23.707 / 22.282 | 15.551 / 14.953 / 14.583 |
| 单次调用P50，三轮ms | 每轮只有1次，不构成分位数认证 | 0.453 / 0.428 / 0.418 |
| 单次调用P95，三轮ms | 同上 | 0.478 / 0.476 / 0.464 |
| 单次调用P99，三轮ms | 同上 | 0.484 / 0.479 / 0.468 |
| 单次最大调用，三轮ms | 22.510 / 22.500 / 21.143 | 0.568 / 0.583 / 0.482 |
| 保留返回slice并GC后HeapAlloc增量bytes | 9171920 / 9127368 / 9127176 | 282928 / 286536 / 286344 |
| 整轮TotalAlloc增量bytes | 13127216 / 13054376 / 13054376 | 12714768 / 12690664 / 12690664 |
| 结束pending | 4096（保留，未同步） | 4096（保留，未同步） |

整轮墙钟包括本轮逐项检查；API调用时间不含检查。GC在计时外显式执行以观察保留slice；HeapAlloc增量不是峰值RSS，不得与累计分配相加。每轮仅32页，分位数精度有限；没有长期持续恢复、CPU/I/O/限流/fsync或业务容量采样。分页限制一次材料化，不能保证任意模型解码和应用汇总堆大小，也没有跨页一致快照。

## 主Agent本人审查

按审查skill从测试、正确性、结构、安全、性能复核：范围绑定防误用但不当身份凭证；游标推进点是最后实际访问键，预算前不读额外value，末页终止；JSON损坏不交付半页，ctx/关闭在循环与返回前检查；无生产写入/新goroutine/依赖变化，85旧文件逐字未变。单次底层Seek/MVCC及任意NewModel不能强杀，页间前方新增可漏本轮，均明确公开边界。未启动任何子Agent，不把自审说成独立审计。

## 升级、回退及NOT_RUN

消费方升级 `go get github.com/digitalwayhk/core@v1.12.0`，无需开关；显式将恢复枚举改为新门面、边页折叠必要状态、空页继续、出错终止并保留原游标。版本发布状态以正式回执为准。`go get`不会安装skill，复制/旧模块软链消费方须按README重同步权威skill。

API只读，不新增磁盘状态，旧API/LocalScanOptions无字段名构造及存量卷字节保持。回退旧模块时应用必须撤销新方法调用；原有终态/失败/ownership格式的旧二进制禁开边界不因本次放松。游标不可跨路径移动，不替代应用准入/fence/重放完整性。

NOT_RUN：Bitzoom/Full-E4接入、数据/CPU/heap原件SHA独立核验、实际恢复OOM及索引完整性、生产UAT/吞吐容量/长稳态、物理断电、真实外部MySQL/Redis/WS集成（本次不改这些路径）、峰值RSS及CPU/I/O/限流/fsync。没有操作消费方仓库、SSH、服务、数据、消息、租约或配置。

## 原始证据

本轮目录 `/private/tmp/core-local-scan-page-20261113/`。原始命令元数据为同名json；测试、基线overlay、API报告、测量摘要与后续正式发布回执通过GitHub Release附件提供。失败原件不改写，日志与代码断言保持。

| 日志（目录内） | exit | bytes | SHA256 |
| --- | --- | --- | --- |
| `red.log` | 1 | 3785 | `5ad8f34901d500b02258878d539f92d01c2edc0c1890d5296a7a15be19cdfb62` |
| `red-old-overlay.log` | 1 | 3785 | `d87bf71e463c81f1978c4a4b7153121e8256b95f9694310e881c38c81402c396` |
| `green-targeted.log` | 1 | 39551 | `5daf37bb0bcf38ad3b013100bc1d64073b27d7be927e1174fae9650638cdad49` |
| `green-contract.log` | 0 | 3379 | `7f0eb583ac446e7cb58e4fc21cf13660a118059037316011137c04d4d758cc8b` |
| `green-targeted-2.log` | 0 | 28289 | `a6b14969c32c9bc52b68523dcf0e746f499fa973bb4118a1980300916daa42e4` |
| `green-final-targeted.log` | 0 | 33946 | `791b01b5c05f1edbc018f61981f8bf6ead6d76b8b7c0c4817260c583578f4f98` |
| `nosql-ordinary.log` | 0 | 2740162 | `5010bdd014b3237a62cbab0f28ab645c9d1aad8fc55395dca4756d39b4148735` |
| `nosql-race.log` | 0 | 2612829 | `897d3fc02d6f71c478e01e17a54cf865225596ed53a7a14f143f4cec0d3b500d` |
| `persistence-unit.log` | 0 | 1215 | `336e03543650ae79a755bb36ff3f820a127b5bc0c0d732b0e8095a2ca815e5da` |
| `examples-ordinary.log` | 0 | 283253 | `c4293ff4aa3f93694dc1c2ca4ab23911e447a7419e3dbe1e15aa11335a9283ae` |
| `examples-race.log` | 0 | 282108 | `300164d7bed00b5af996ca93e45c8b373ec4b2ed2514233f1465dc24147c4cfc` |
| `api-old-compat.log` | 0 | 308 | `f6465839fdd559c44592c8bdf3fb025583babb8250182d834c96770755ece77d` |
| `release-contract.log` | 0 | 1357 | `139cf2733f8c483be66554d715718a7f4f2b64e6f6eafca06be7884e03caa8fb` |
| `performance-contract.log` | 0 | 326 | `76bf7eeedb5ed65f1aac4298d700822597c1efef38acfc39fe9c73a104bdcc96` |
| `web-dist-sync.log` | 0 | 176 | `cb998eea575d2a081076ce4898622a10c25c2b925a6cde1b414c63a956e9fb5b` |
| `logging.log` | 0 | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `measure-legacy.log` | 0 | 4572 | `92f71292825eb82fe56a706907d3acbd29461146794f8515b62be4c578ab2a61` |
| `measure-page.log` | 0 | 4549 | `57805c95a23eab3652c5589f81364861d193f4f9e975c31958f6280b6a7222e1` |
