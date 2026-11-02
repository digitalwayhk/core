# 归属WS退出竞态修复

基线v1.8.0 / 0a26e8a0，分支fix/ownership-ws-shutdown，当前主Agent单独执行。仅Core，不修改Bitzoom/Full-E4/Pebble或现有WIP。

- [x] 核对真实源码、规范、生命周期及消费方原始栈。
- [x] 正式ServiceContext+真实Redis+原生Gateway取得RED；RegisterOwnedKey/CallServiceOwned和invokePayload同类访问取得RED。
- [x] 心跳复用已有服务级同步转发器注册表；MQ与Resolver使用生命周期锁快照；引用worker取消并等待真实结束。
- [x] 取消/关闭/恢复、公开API、普通/race及真实四进程SQL/WS/迁移回归；保留失败轮次。
- [x] 更新权威skill、指南、原始证据和升级记录。
- 正式发布按clean门禁、main、annotated tag与官方Go代理回执逐项核验，精确状态见最终交付。

不增队列/线程/业务状态，不更改归属、消息、SQL fence、mTLS和普通WS合同。注册不等于Ready，退出超时不等于真实完成。

发起者同轮补充的内部白名单500/403分类已按真实HTTP RED→GREEN实施，不改变白名单；真实双Gateway mTLS另取证。主Agent还通过真实RED确认入站摘要的同一broker退出竞争与取消先于lease清理边界，均最小修复并保留原件。
