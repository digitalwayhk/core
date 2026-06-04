# 09 - Transport Selector

本示例展示内部传输选择器：

- 已实现：`grpc`、`http`、`socket`。
- 配置可识别但当前未实现为 direct transport：`quic`、`mq`。
- 当 `Internal=quic` 或 `Internal=mq` 时，框架会 fail fast，避免静默退回 HTTP。

## 运行

```bash
go run ./examples/09-transport-selector/main
```

示例会构造一个 `grpc -> http -> socket` selector，并演示 `mq` 作为主传输时的错误。
