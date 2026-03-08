# 停止并删除旧容器（如果存在）
docker stop mysql
docker rm mysql

# 创建新容器
docker run -d \
  --name mysql \
  --restart=always \
  --log-opt max-size=50g \
  --log-opt max-file=3 \
  --memory="2g" \
  --cpus="2" \
  -p 9001:3306 \
  -e MYSQL_ROOT_PASSWORD=futures_2026_Tes! \
  -e MYSQL_DATABASE=default \
  -e MYSQL_USER=default \
  -e MYSQL_PASSWORD=futures_2026_Tes! \
  -e TZ=Asia/Shanghai \
  -v /data/mysql/data:/var/lib/mysql \
  -v /data/mysql/conf:/etc/mysql/conf.d \
  mysql:8.0

# 验证容器运行
docker logs mysql --tail 50

# 测试连接
docker exec -it mysql mysql -udefault -p