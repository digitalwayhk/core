# 前台空组核验窄修复实施记录

由当前主 Agent 独立执行，禁止分派子智能体。用户已经授权开发、验证和独立提交；不自动合并或发布。

目标：保持可靠写语义和卷格式，消除同组版本替换的无效核验，将可能变空的组限定在每个实际事务最终写集上去重处理，底层迭代器只查目标组。

- [x] 核对当前 HEAD、191项原WIP及现场CPU原件指纹。
- [x] 在原算法加入包内局部只读观测，扩展真实Badger测试；记录同组重复核验和缺少底层Prefix的真实RED。
- [x] 使用同一测试夹具构建修复前二进制，封存源码与二进制指纹。
- [x] 将ensureDispositionEntry/ensureSyncQueueEntry的内部deferredGroups传递到ApplyWriteOperations及BatchInsert；集合在实际事务闭包内建立，ErrTxnTooBig重试重建。
- [x] 比较旧新实际组身份；同组替换不查空组，跨组在最终写集上核验。每个目标组使用带Prefix的新迭代器并及时关闭。
- [x] 覆盖跨组、最终删除再加入、完成清理、回滚、实际TooBig拆分及部分前缀、在途/失败屏障和重开恢复。
- [x] 保持SyncWrites=true、P数/介质/载荷/合批/历史规模一致，交替运行修复前后持续输入及CPU诊断，不并行编译污染计量；逐轮清理合成卷。
- [x] 运行nosql及04/07普通/race、persistence-unit/config-contract/performance-contract/release-contract和日志门禁。
- [x] 自查事务可见性、回滚及兼容性，更新权威skill/正式指南，封存证据并提交本任务白名单文件。消费方和Full-E4为NOT_RUN。
