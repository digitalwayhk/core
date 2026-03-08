# 停止并删除旧容器
docker stop clickhouse
docker rm clickhouse

# 创建新容器（带日志大小限制）
docker run -d \
  --name clickhouse \
  --restart=always \
  --log-opt max-size=80g \
  --log-opt max-file=3 \
  --ulimit nofile=262144:262144 \
  --memory="2g" \
  --cpus="2" \
  -p 9002:9000 \
  -p 9003:8123 \
  -e CLICKHOUSE_DB=default \
  -e CLICKHOUSE_USER=default \
  -e CLICKHOUSE_PASSWORD=futures_2026_Tes! \
  -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  -e TZ=Asia/Shanghai \
  -v /data/clickhouse/data:/var/lib/clickhouse \
  -v /data/clickhouse/logs:/var/log/clickhouse-server \
  clickhouse/clickhouse-server:latest

# 验证容器运行
docker logs clickhouse --tail 50