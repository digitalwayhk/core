# 已就绪写入的批收集优化

基线：v1.6.2 / 3d8eade96c24e1738174b842ad059cfad5e3d546；独立分支fix/write-batch-ready-collection，保留190项WIP。只处理BatchCommitter前台收集阶段，不扩入元数据状态优化、业务协议或依赖升级；主Agent独立执行，不调用子Agent。

- [x] 核对现有run、关闭、部分前缀、内部命令FIFO及先前慢样本。
- [x] RED：真实已有3个请求、MaxBatch=8、CollectWindow=1小时；旧实现收完已有请求后等待尚不存在的未来请求，严格合批/及时提交断言失败。
- [x] 候选A实测否决：队列暂空立即提交使短窗事务369→443、前台批次47→122；持续批次1110→1702。保留A源码、原RED/GREEN与指标，不作为最终方案。
- [x] 候选B最小改动：只删除无条件Gosched，保留原有限CollectWindow、条数预算和内部事务FIFO边界。新版测试仅识别实际Gosched运行栈，不把异步抢占误判成主动让出；初版race断言失败证据保留。
- [x] 按用户补充要求查固定Badger v4.9.6官方排障/Options，导出实际Options，在原生产逻辑下一次只改变memtable、vlog、合批窗口或compactor，并实测；SyncWrites/DetectConflicts不变。
- [x] 最终nosql及04/07普通与race各438 PASS/50 SKIP/0 FAIL，含SIGKILL新进程恢复；保留FIFO边界正式回归。未采纳的候选设计测试连同原失败证据保存于实验快照。
- [x] 22组共728000次Save，错误0；18个后台组pending=0，4个无后台控制组保留2000合成pending并在封存后清卷。两个调度候选均无采用依据，生产实现恢复精确基线。两轮参数与反向顺序控制完成，不改默认值。
- [x] 更新正式指南/skill和中文证据报告；普通/race与契约门禁均完成，形成独立可审提交。不自动合并或发布；不替消费方升级或测试。
- [x] 每轮封存结果/命令/文件SHA后立即回收测试卷；最终五个二进制共260239360已分配字节已删除，合成数据文件为0，只留小结果/日志。

旧有关闭、接受后等待真实提交、版本/指纹、组屏障、SyncWrites/冲突检测、pending/清理/预算合同保持。Full-E4、业务UAT/容量、全仓普通/全仓race与OS同步调用统计分别标NOT_RUN。
