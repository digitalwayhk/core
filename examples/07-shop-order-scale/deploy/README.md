# 07 Docker 水平扩展说明

本目录演示 07 订单服务的多副本部署约束。Compose 文件提供固定 `shop-order-a/b` 便于 UAT 断言，也提供 `shop-order` scale 模板用于编排层扩容实验。

水平扩容必须满足：

- 所有 order 副本使用相同 `ServiceName=shop-order`。
- 每个副本通过 `AutoMachineID=true` 自动获得唯一 MachineID。
- 每个副本拥有唯一 ServiceInstanceID。
- 每个副本拥有独立本地 pending 目录。
- 所有副本共享同一个远程 order 权威库。
- 注册发现 Provider 由配置决定，业务代码不能写死 Redis。
- order 副本不暴露宿主机业务端口，内部调用走 ServiceResolver。
- 缩容时先停止接新请求，再尽量 drain 本地 pending；未 drain 完的 pending 必须可恢复。

固定双副本：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml up shop-user shop-supplier shop-order-a shop-order-b
```

scale 模式：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml --profile scale up --scale shop-order=3
```

`shop-order` scale 模板故意不声明 named volume。Compose 使用 `--scale` 时，同一个服务定义中的 named volume 会被多个副本共享，这会破坏“每个副本独立本地 pending”的约束；需要持久化时应由编排平台按副本注入独立卷。
