#!/bin/bash

# MySQL 测试环境启动脚本

echo "🚀 启动 MySQL 测试环境..."

# 停止并删除旧容器
docker stop mysql-test 2>/dev/null
docker rm mysql-test 2>/dev/null

# 启动 MySQL 容器
docker run -d \
  --name mysql-test \
  -e MYSQL_ROOT_PASSWORD=test123456 \
  -e MYSQL_DATABASE=test_db \
  -p 3307:3306 \
  mysql:8.0 \
  --character-set-server=utf8mb4 \
  --collation-server=utf8mb4_unicode_ci

echo "⏳ 等待 MySQL 启动..."
sleep 15

# 检查 MySQL 是否就绪
docker exec mysql-test mysqladmin ping -h localhost -uroot -ptest123456 --silent

if [ $? -eq 0 ]; then
    echo "✅ MySQL 测试环境已就绪"
    echo "📊 连接信息:"
    echo "   Host: localhost"
    echo "   Port: 3307"
    echo "   User: root"
    echo "   Password: test123456"
    echo ""
    echo "🧪 运行测试:"
    echo "   go test -v ./pkg/persistence/database/oltp -run TestMySQLSuite"
else
    echo "❌ MySQL 启动失败"
    exit 1
fi