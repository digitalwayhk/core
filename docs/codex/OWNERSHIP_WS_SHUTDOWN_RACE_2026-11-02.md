# v1.8.1 归属WS退出竞态修复与验收

## 范围与基线

正式基线v1.8.0 `0a26e8a013358e67907fce6fde079e10fb914b87`，独立分支fix/ownership-ws-shutdown；仅Core当前主Agent实施，未操作Bitzoom/Full-E4，也未修改Pebble实验或原WIP。消费方原栈和文档只读，原件保持不变。

## 根因与最小改动

ServiceContext.SetRunState(false)在lifecycleMu内把CrossNodeBroker、MQManager和ServiceResolver置空，再关闭既有资源。引用心跳flush/ensureOwnershipReferences在锁外直接读这些可清空字段。旧代码虽有nil检查，检查和使用既不同步，也不保证对象仍是同一个。

实际RED还确认RegisterOwnedKey、CallServiceOwned和Ready调用进入的invokePayload存在同类字段访问。此锁为进程内生命周期状态锁；本轮不把Redis/SQL/网络调用放入锁，也没有改变金融事务或消息ACK。

- melody/ownership_gateway_runtime.go：flush复用既有types.GetCrossNodeForwarderForService按服务同步注册表，取得当前已有中继。run使用可取消生命周期；停止取消在途刷新，等待其真正返回，再关闭Gateway lease和worker。关闭后flush拒绝，不能再次开户或写快照。
- melody/manager.go、ownership_references.go：启动组合根取得稳定MQManager引用，后续不从后台读取可清空公开字段；仍由MQManager/Provider原关闭合同校验连接。
- router/owned_events.go、owned_service.go：生命周期锁内快照manager/terminated，锁外调用，关闭中新调用明确NotReady；未接MQ仍Unsupported。
- router/owned_websocket_relay.go：同样通过真实RED确认入站摘要在停止时读取CrossNodeBroker，改用原生命周期锁快照并拒绝终态；同服务来源/节点检查与普通WS回退不变。
- router/servicecontext.go invokePayload：相同锁内快照resolver/terminated，避免多次读取时被置空；停止后Unavailable，不自动重试未知副作用。
- 示例11：真实恢复另暴露Runtime.snapshot在SetRunState(true)发布broker期间读取未完成对象，改用相同同步索引；应用示例Initialize取得原ownership.Store，后续Read/Notice复用同一Store，未引入新协议。
- 现有integration.Suite.StopProcess返回真实退出错误，退出race和5秒未完成后的强杀不能计PASS。两个既有多进程Suite在清理后同时核对退出值及完整race日志。测试helper不属于Core生产API。

现有3秒刷新/128摘要上限、mTLS、同服务来源/节点校验、普通WS回退、用户隔离、退订/关闭和原引用恢复保持。未改变Stream/Group/PEL/消息ID/OrderingKey/epoch/SQL fence/动态目录指纹，也未新增提交器、队列或工作线程。

## RED原件

所有shell使用rtk。独占Redis56392/56394，本专项MySQL56393，配置凭据仅存0600临时文件，文档和归档不含秘密。

`CORE_TEST_REDIS_ADDR=127.0.0.1:56392 go test -race ./pkg/server/trans/websocket/melody ./pkg/server/router -run 'Test(GatewayReferenceFlushConcurrentServiceShutdown|OwnedPublicCallsConcurrentServiceShutdown)$' -v -count=1 -timeout 90s`：red.log退出1，Gateway、RegisterOwnedKey和CallServiceOwned均报告DATA RACE。

red-r2中Gateway受旧夹具重复前缀影响为SETUP_FAIL，Router为真实RED；改用每轮独立命名空间后red-r3退出1，另确认invokePayload的Resolver竞争。未把夹具失败当有效RED。

最终测试通过精确v1.8.0六生产文件只读overlay再次运行，red-final-overlay.log退出1：Gateway flush/ensure、Register/Call/Resolver报告race；真实Redis快照提交后受控阻塞的关闭测试返回DeadlineExceeded（原刷新未被取消）。overlay和源文件SHA随证据保存，不改模块缓存或回退当前源码。

首轮真实进程processes.log整轮FAIL：动态及固定迁移的业务断言完成，但退出检查发现示例11启动snapshot/broker发布竞态。首轮ws-relay因运行期间测试helper更新出现旧返回签名编译错误，单列SETUP_FAIL；不计WS验收。全部原件保留，后续从一致源码重新运行。

## 已运行回归

- green-lifecycle.log：加强后的目标race连续5轮PASS，真实Redis和正式ServiceContext；受控取消确认刷新真正返回后才关闭lease，关闭后不能再写。
- packages.log：ownership/mq/event/router/cluster/WebSocket/rest完整普通PASS；MQ17.315s、event22.700s、router12.841s、cluster14.380s、melody9.617s、rest1.020s。
- race.log：相同完整包race PASS；MQ19.682s、event29.159s、router14.104s、cluster20.186s、melody10.962s、rest2.285s。
- helper-race.log：真实子进程退出0与66的结果准确传回、重复关闭幂等，race PASS。
- example-race.log：示例11各目录仅编译，无测试，明确COMPILE_ONLY，不能代替下述真实进程证明。

- processes-final.log：关闭修复及示例修正后，真实四进程固定85.19s、动态114.48s、固定→动态117.76s，完整317.933s PASS；双Gateway普通/归属WS与mTLS拒绝29.638s PASS。均检查实际退出错误和退出日志。追加拒绝分类及入站锁快照后，processes-release.log完整PASS：固定83.70s、动态97.50s、迁移115.81s，示例11合计298.123s；双Gateway35.171s，包含真实匿名有效/损坏JSON403、Parse=0、同服务mTLS可用及异服务拒绝。

## 追加的预期拒绝分类

发起者同轮补充真实匿名内部白名单HTTP500，要求403。types.ErrInternalCallerForbidden原为裸errors.New，ResolvePublicError默认Internal。生产最小修复仅将同一导出变量明确保留error静态类型，使用现有Forbidden/PublicCodeForbidden及原原因，保留errors.Is；权限、来源验证、Parse前检查和OpenAPI隐藏规则不变。

forbidden-red.log中真实HTTP夹具初始路径未跟随ServiceRouter冻结为404，单列SETUP_FAIL；forbidden-red-r2.log路径修正后出现真实403预期/500实际，错误链不是PublicError，退出1。forbidden-green.log目标race PASS，实际HTTP有效/损坏JSON均403且Parse=0，错误链保留；合成可信context仅作单元流程证明，真实mTLS另在最终双Gateway测试。

## 最终增量关闭顺序

主Agent受控测试在已接纳握手仍未排空时关闭Gateway：cancel-order-red.log确认旧候选先CloseGateway再取消，断言需要context.Canceled却得到nil，退出1。修正仅把原worker退出的取消前移至lease清理之前，先拒绝迟到刷新，再等待持有flush互斥的实际调用返回，最后CloseGateway/done；不新增线程或超时预算。

cancel-order-normal.log完整melody普通PASS 9.840s；cancel-order-green.log完整race PASS 11.210s。最后增量源码的真实动态四进程processes-cancel-final.log再次通过：99.373s，原WS换主免重订、SQL fence和子进程实际退出均核验；双Gateway31.561s PASS，新增真实HTTP403与mTLS允许/拒绝、普通/归属WS及退出均通过。

最终完整普通packages-release.log、完整race race-release.log含types/ownership/mq/event/router/cluster/WebSocket/rest及原进程helper，均PASS；8项gate-results-release.json全为0。没有将零测试编译算行为证明。

## 主Agent自审

复核停止/启动与回调的所有已触发字段访问、manager/Resolver快照、registry锁、取消和实际退出顺序；未在外部调用持有生命周期锁，未建立新线程/队列；原安全身份、原消息/PEL、SQL fence和恢复边界保留。生产公开API及配置无加性变更，变量静态类型为error；测试helper的返回错误只影响验收准确性。go.mod/go.sum、MQ Provider、持久化和前端无修改。

最终发布信息、正式clean门禁及Go代理冷缓存结果由精确tag与交付回执核验，未执行前不计PASS。

## 兼容与消费方

补丁不新增生产API、配置或持久格式；从v1.8.0升级无需改接线或清任何数据。已有固定/动态scope保持各自冻结合同，v1.8.0规定的维护迁移及禁止新旧模式混跑/直接降回固定限制不变。刷新权威skill链接；使用正式RegisterOwnedKey、CallServiceOwned，应用后台不得反复无锁读取ServiceContext公开字段。已接纳调用遇关闭仍可能结果未知，不自动重试副作用。

Core组件通过不代表消费方接入或业务通过；Bitzoom/Full-E4升级、部署、UAT和容量均NOT_RUN。未新做性能实验，不宣称吞吐/P99提高。不合作远端业务调用不能被租约或取消强杀，退出超时不是实际退出成功。

## 原始证据归档

`docs/codex/evidence/ownership-ws-shutdown-20261102/raw-evidence.tar.gz`，54600 bytes，SHA256 `850bbc631a4a4d82f8f2e27633698edbf33e91a3ff3c1993049ba2507ef31b42`；含60份原件，原始合计368960 bytes，逐项path/bytes/SHA256见归档raw-manifest.json。密码、证书、数据库及消费方业务文件未归档。正式clean门禁及Go代理下载回执另存本专项临时证据目录，不循环写入自身提交SHA。
