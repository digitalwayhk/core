#!/bin/bash
# filepath: /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/futures/scripts/check_redis_pending.sh

echo "🔍 检查 Redis Stream Pending 消息..."

STREAMS=$(redis-cli --scan --pattern "*:*" | grep -E "(requests|responses)")

for stream in $STREAMS; do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Stream: $stream"
    
    # 检查 Stream 长度
    LENGTH=$(redis-cli XLEN "$stream" 2>/dev/null || echo "0")
    echo "   长度: $LENGTH"
    
    # 检查消费者组
    GROUPS=$(redis-cli XINFO GROUPS "$stream" 2>/dev/null)
    
    if [ -z "$GROUPS" ]; then
        echo "   无消费者组"
    else
        echo "$GROUPS" | grep -oP 'name \K\S+' | while read group; do
            echo "   消费者组: $group"
            
            # 检查 Pending 消息
            PENDING=$(redis-cli XPENDING "$stream" "$group" 2>/dev/null)
            echo "      Pending: $PENDING"
            
            # 详细 Pending 信息
            redis-cli XPENDING "$stream" "$group" - + 10 2>/dev/null | while read line; do
                echo "         $line"
            done
        done
    fi
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"