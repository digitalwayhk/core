# 07 Docker 水平扩展说明

本目录演示 07 订单服务的多副本部署约束。Compose 文件提供固定 `shop-order-a/b` 便于 UAT 断言，也提供 `shop-order` scale 模板用于编排层扩容实验。

水平扩容必须满足：

- 所有 order 副本使用相同 `ServiceName=shop-order`。
- 每个副本通过 `AutoMachineID=true` 自动获得唯一 MachineID。
- 每个副本拥有唯一 ServiceInstanceID。
- 每个副本拥有独立本地 pending 目录。
- 所有副本共享同一个内部 MySQL 远程 order 权威库，Compose 不向宿主机暴露 MySQL 端口。
- 注册发现 Provider 由配置决定，业务代码不能写死 Redis。
- order 副本不暴露宿主机业务端口，内部调用走 ServiceResolver。
- 缩容时先停止接新请求，再尽量 drain 本地 pending；未 drain 完的 pending 必须可恢复。

固定双副本：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml up shop-user shop-supplier shop-order-a shop-order-b
```

固定双副本会同时启动 `mysql` 和 `redis` 依赖；`shop-order-a/b` 都通过 `SHOP_ORDER_REMOTE_MYSQL_*` 指向同一个 `shop_order_scale_remote` 权威库。

scale 模式：

```bash
docker compose -f examples/07-shop-order-scale/deploy/docker-compose.yml --profile scale up --scale shop-order=3
```

`shop-order` scale 模板故意不声明 named volume。Compose 使用 `--scale` 时，同一个服务定义中的 named volume 会被多个副本共享，这会破坏“每个副本独立本地 pending”的约束；需要持久化时应由编排平台按副本注入独立卷。

scale 模板也不设置 `SHOP_ADVERTISE_ADDRESS`，运行时会使用容器 hostname 作为发现地址，避免多个副本注册同一个静态地址。模板不传固定 `-p/-grpc` 参数；如果编排平台要求覆盖容器内监听端口，应使用 `SHOP_ORDER_HTTP_PORT/SHOP_ORDER_GRPC_PORT` 环境变量。

真实 Docker 多副本 UAT 默认不在普通测试中启动；需要验证完整链路时运行：

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run TestDockerComposeOrderScaleUAT -count=1 -v
```

按角色单独验证时可以运行：

```bash
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run TestDockerUATBuyerRoleFlow -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run TestDockerUATSupplierRoleFlow -count=1 -v
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run TestDockerUATAdminRoleFlow -count=1 -v
```

Docker UAT 会读取 Redis discovery 中的 `shop-order` 节点，确认两个副本拥有不同 `MachineID` 和 `ServiceInstanceID`；买家角色还会用相同 `requestID` 重试下单，确认入口层幂等不会返回漂移的订单号。
