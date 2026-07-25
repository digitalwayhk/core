# gRPC 证书挂载目录

此目录只是部署挂载点，不是证书仓库。禁止提交 CA 私钥、服务私钥或已签发证书。

启动 Compose 示例前，应通过部署密钥管理器提供以下文件：

```text
ca.crt
shop-user.crt
shop-user.key
shop-supplier.crt
shop-supplier.key
shop-order.crt
shop-order.key
```

服务证书必须由 `ca.crt` 签发，同时允许服务端和客户端认证，并把逻辑服务名
（`shop-user`、`shop-supplier` 或 `shop-order`）写入 DNS SAN。`{service}`
服务器名称模式会逐次调用校验该身份；共享的 `localhost` SAN 不能替代逻辑服务名。

私钥权限应为 `0600`，证书权限应为 `0644`。Compose 会把本目录只读挂载到
`/run/secrets/shop-grpc`。生产环境应通过密钥管理器注入短期身份，或使用能够提供
等价双向身份验证的 service mesh；仓库不会提供任何可直接使用的证书或私钥。
