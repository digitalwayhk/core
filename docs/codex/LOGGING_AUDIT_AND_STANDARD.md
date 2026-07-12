# 日志审计与规范

## 当前审计

| 类别 | 已确认位置 | 风险 | 目标 |
| --- | --- | --- | --- |
| stdout/标准 logger | `utils`、`router`、`run`、`trans/rest`、`trans/socket`、QUIC | 绕过级别、字段、采集与脱敏 | 删除或改为结构化 `logx` |
| panic/fatal | QUIC、REST 启动路径及历史认证构造器 | 可复用库终止进程 | 返回错误或交给生命周期边界 |
| payload/response/object dump | `servicecontext.go`、socket、ModelList、WebSocket | 泄露业务数据和凭据 | 仅记录类型、大小、目标和错误类别 |
| 原始 SQL/参数 | GORM、ClickHouse 查询日志 | 泄露数据值且高频 | 记录 operation/table/duration/error，不记录 SQL 文本和参数 |
| 装饰图标/横幅 | Server、SQLite/MySQL、Badger、ClickHouse、WebSocket | 难检索、事件名不稳定 | 使用 ASCII `snake_case` 事件名 |
| 重试/回退级别 | cluster、transport、persistence | 可恢复尝试被记为 error | attempt=debug，成功回退=info，耗尽=error |
| TraceID 绑定 | Request 已传播，日志很少绑定 | 无法按请求定位失败 | 请求边界使用 context fields，跨服务沿用 payload TraceID |

## 运行时契约

本仓库只使用 go-zero `logx`，不增加第二日志门面。事件名稳定、字段结构化、每个错误仅在责任边界记录一次。面向客户端的本地化错误不等同于运行时日志事件，日志事件统一使用英文 ASCII 名称。

## 临时例外

任务 8 分批迁移期间，静态守卫只检查已确认的高风险语法，不以全局 allowlist 掩盖发现。仍未结构化的 `logx.Infof/Errorf` 会由各批台账逐项收敛；任务关闭前不得以“历史代码”为理由保留敏感值、stdout、标准 logger 或装饰图标。
