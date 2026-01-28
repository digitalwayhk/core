# ==================== ClickHouse 测试环境启动脚本 ====================

# 1. 清理旧容器
docker rm -f clickhouse-test 2>/dev/null || true

# 2. 启动 ClickHouse（设置空密码以允许访问）
docker run -d \
  --name clickhouse-test \
  -p 8123:8123 \
  -p 9000:9000 \
  -e CLICKHOUSE_DB=default \
  -e CLICKHOUSE_USER=default \
  -e CLICKHOUSE_PASSWORD=clickhouse \
  -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  --ulimit nofile=262144:262144 \
  clickhouse/clickhouse-server

# 3. 等待启动完成
echo "等待 ClickHouse 启动..."
sleep 10

# 4. 验证连接
echo "验证连接..."
docker exec clickhouse-test clickhouse-client --query "SELECT version()"

# 5. 测试是否可以创建数据库
docker exec clickhouse-test clickhouse-client --query "CREATE DATABASE IF NOT EXISTS test"
docker exec clickhouse-test clickhouse-client --query "SHOW DATABASES"

# 6. 查看日志确认启动成功
echo "查看启动日志..."
docker logs clickhouse-test 2>&1 | tail -20

# ==================== 运行测试 ====================

# 运行完整套件
go test -v -run TestClickHouseSuite ./pkg/persistence/database/olap/

# 快速测试(跳过性能测试)
go test -v -short -run TestClickHouseSuite ./pkg/persistence/database/olap/

# 运行所有测试
go test -v ./pkg/persistence/database/olap/

# 运行所有测试
go test -v ./pkg/persistence/database/olap/

# 运行特定测试
go test -v -run TestClickHouseConnection ./pkg/persistence/database/olap/

# 运行时间维度测试
# go test -v -run TestTimeBasedViews ./pkg/persistence/database/olap/

# 运行业务维度测试
# go test -v -run TestBusinessView ./pkg/persistence/database/olap/

# 运行性能测试
# go test -bench=. -benchmem ./pkg/persistence/database/olap/

# 运行压力测试（耗时较长）
# go test -v -run TestStress ./pkg/persistence/database/olap/

# 跳过压力测试
# go test -v -short ./pkg/persistence/database/olap/

# ==================== 调试命令 ====================

# 进入 ClickHouse 容器
# docker exec -it clickhouse-test bash

# 进入 ClickHouse 客户端
# docker exec -it clickhouse-test clickhouse-client

# 查看容器日志
# docker logs -f clickhouse-test

# 查看配置文件
# docker exec clickhouse-test cat /etc/clickhouse-server/config.xml

# 查看用户配置
# docker exec clickhouse-test ls -la /etc/clickhouse-server/users.d/

# ==================== 清理 ====================

# 停止并删除容器
docker rm -f clickhouse-test

# 删除所有 ClickHouse 相关容器
# docker ps -a | grep clickhouse | awk '{print $1}' | xargs docker rm -f