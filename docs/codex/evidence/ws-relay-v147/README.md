# v1.4.7 原始RED

- `red-04.txt`：未修改v1.4.6生产实现，真实双Gateway/mTLS/独占Redis/WS；普通Private和Public的摘要索引及notice均失败，归属路由通过；旧HTTP中继匿名/用户Token错误成功。两个生产文件SHA与797781f逐字核对。
- `registered-servermanage-red.txt`：普通路由兼容修复后，自审强化为真实已注册GetMenu，检出同服务mTLS错误取得管理访问。它不是基线整套结果。
- `compress-red.txt`：v1.18.6接受溢出S2字典，最低安全版本门禁也失败；没有执行可能SIGSEGV的Encode。
- `manifest.json`：上述原始输出SHA256及最初RED源码哈希；输出包含的用户A/B、序列、节点名仅为隔离夹具，不含业务数据、私钥或Token。

最初三轮夹具问题（编译、端口组装、错误读取gRPC偏移）保留在本机`.temp/ws-relay-regression-20261022/red-01.log`至`red-03.log`，不算有效RED。`red-04`使用服务发现的实际gRPC端口和AddServerRouters正式注册。GREEN沿用原断言，之后增加了反向通知、异服务节点、精确HTTP403及已注册管理路径拒绝，未减弱断言。详细命令、GREEN和未运行边界见[发布说明](../../RELEASE_V1_4_7_2026-10-22.md)。

原始输出按字节保存，包括testify的尾部空格、空格/Tab混排和终端回车；不为格式检查修改RED。源码/文档的`git diff --check`单独排除这些原始`.txt`输出，哈希用于核对其原样保存。
