# 11 - Cluster Etcd Consul

本示例展示如何为集群注册发现切换到 etcd 或 Consul。

代码只构造配置并校验，不主动连接外部服务；真实运行时由 `ServiceContext` 根据配置创建 provider。

## etcd Docker

```bash
docker run --rm --name core-etcd \
  -p 2379:2379 \
  quay.io/coreos/etcd:v3.5.18 \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

## Consul Docker

```bash
docker run --rm --name core-consul \
  -p 8500:8500 \
  consul:1.17 agent -dev -client=0.0.0.0
```

## 运行

```bash
go run ./examples/11-cluster-etcd-consul/main
```

## 切换建议

- 开发和默认运行：`Provider=local`。
- 需要多进程/多主机注册发现：`Provider=etcd` 或 `Provider=consul`。
- 动态切换时使用 ProviderSwitcher 的 begin / complete / rollback，先双读或观察，再完成切换。
