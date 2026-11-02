# 通用持续归属动态key修复实施记录

基线：main/v1.7.1=4f88a3d9；独立分支fix/ownership-dynamic-keys。用户已授权完整实施及通过正式门禁后发布；主Agent独立执行，不派生子Agent。Pebble与消费方不在范围。

## 实施顺序

- [x] 核对分支、HEAD、WIP及远端main/tag；另建36b9隔离工作区。
- [x] 读权威skill及真实调用链；保存真实Redis旧源码RED：未知key污染已知观察、空动态目录被拒绝。
- [x] 保持固定Manifest JSON/Fingerprint，增加显式DynamicKeys冻结预算；新Store可选KeyRegistry及正式Provider/Manager/ServiceContext注册入口。
- [x] 原子幂等追加目录、分页发现、Runtime持续发现；Key回调只分类消息，不授予注册权限。
- [x] 未登记消息原PEL隔离、原ordering-key屏障；动态模式有限预留窗口，原W与新预算分别声明，总PEL硬限W+UnregisteredWindow，超出仍背压。
- [x] 停稳后的固定→动态原子迁移、Recover/Unbind遍历动态目录并拒绝活动租约，保留SQL下界、原组/PEL/游标。
- [x] RED/GREEN、普通/race、原模式、真实Redis/MySQL/双处理进程/双Gateway/mTLS/WS与故障/迁移验证。
- [x] 原新固定资源对照及证据、skill/API/示例11/迁移说明，独立提交并自审。
- [ ] clean正式发布门禁、main快进/合并、v1.8.0 annotated tag及远端/Go代理核验（最终交付核对，不以candidate门禁冒充已发布）。

## 已确认的结构边界

`mapMessages`遇不在固定Keys的合法key返回ErrUnknown；watchKey将整个当前key观测改为-1。Runtime.run只遍历启动Keys。声明指纹冻结Keys，无正式注册路径。

`subjectMessages`及`redisSharedReadScript`将原组总PEL限制为W；W条未登记前序占满时不可能在保持总PEL严格W、保留原消息且不跳游标的同时读取后方健康消息。动态模式显式增加有限UnregisteredWindow作为冻结合同，原W与固定模式不变，动态总预算改变必须文档、生命周期容量校验及迁移签收。已通过Orca向发起者报告，不能称为无限隔离或性能认证。

## 验证状态

旧源码RED命令：`CORE_TEST_REDIS_ADDR=127.0.0.1:56389 go test ./pkg/server/mq -run "TestRedisOwnership(UnregisteredKeyCannotPoisonKnownObservation|DynamicManifestCanStartWithoutFutureKeys)$" -v -count=1`；退出1，两个真实行为断言失败，非编译失败。原件`.temp/ownership-dynamic-20261102/red.log`。

本地Redis为本任务新建56389独占进程。其余验证在实际执行前均为NOT_RUN。禁止以组件测试代替Bitzoom/Full-E4验收。

功能/普通/race/真实Redis/MySQL/多进程/mTLS/WS、同资源组件对照与原失败证据已封存，见正式修复记录。代码发布之后的消费方接入、Full-E4/UAT/容量仍为NOT_RUN。
