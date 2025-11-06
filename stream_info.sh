#!/bin/bash
# 检查Stream状态

echo "🔍 检查Redis Stream..."

# 列出所有Stream
echo "当前Stream列表:"
redis-cli --scan --pattern "*stream*"

# 检查Stream长度
for stream in $(redis-cli --scan --pattern "*stream*"); do
    echo "Stream: $stream"
    redis-cli XLEN "$stream"
    redis-cli XINFO STREAM "$stream" 2>/dev/null || echo "  无法获取Stream信息"
done

# 检查阻塞的消费者
echo ""
echo "检查消费者组:"
for stream in $(redis-cli --scan --pattern "*stream*"); do
    redis-cli XINFO GROUPS "$stream" 2>/dev/null
done

# 检查待处理消息
echo ""
echo "检查待处理消息:"
for stream in $(redis-cli --scan --pattern "*stream*"); do
    redis-cli XPENDING "$stream" "group_name" 2>/dev/null
done