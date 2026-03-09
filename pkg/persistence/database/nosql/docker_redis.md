# ==================== Redis Docker 启动脚本 ====================

# DSN: redis://:futures_2026_Tes!@192.168.0.187:9005/0

# 停止并删除旧容器（如果存在）
docker stop redis
docker rm redis

# 创建新容器
docker run -d \
  --name redis \
  --restart=always \
  --log-opt max-size=50g \
  --log-opt max-file=3 \
  --memory="1g" \
  --cpus="1" \
  -p 9005:6379 \
  -v /data/redis/data:/data \
  redis:latest \
  redis-server \
  --requirepass "futures_2026_Tes!" \
  --appendonly yes \
  --appendfsync everysec \
  --maxmemory 900mb \
  --maxmemory-policy allkeys-lru

# 验证容器运行
docker logs redis --tail 30

# 测试连接
docker exec -it redis redis-cli -a "futures_2026_Tes!" ping

# 测试选择 db 0
docker exec -it redis redis-cli -a "futures_2026_Tes!" -n 0 info server

# ==================== 调试命令 ====================

# 进入 Redis 客户端
# docker exec -it redis redis-cli -a "futures_2026_Tes!"

# 查看日志
# docker logs -f redis

# 查看内存使用
# docker exec -it redis redis-cli -a "futures_2026_Tes!" info memory

# 查看所有 key（慎用生产环境）
# docker exec -it redis redis-cli -a "futures_2026_Tes!" keys "*"

# ==================== 清理 ====================

# 停止并删除容器
docker stop redis && docker rm redis
