#!/bin/bash
# filepath: /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/futures/scripts/clean_redis_complete.sh

echo "🧹 完整清理 Redis Stream 和消费者组..."

# ✅ 1. 获取所有 Stream
STREAMS=$(redis-cli --scan --pattern "*:*" | grep -E "(requests|responses)")

for stream in $STREAMS; do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔍 处理 Stream: $stream"
    
    # ✅ 2. 获取该 Stream 的所有消费者组
    GROUPS=$(redis-cli XINFO GROUPS "$stream" 2>/dev/null | grep -oP 'name \K\S+' || echo "")
    
    if [ -z "$GROUPS" ]; then
        echo "   ⚠️  没有消费者组"
    else
        for group in $GROUPS; do
            echo "   🔍 消费者组: $group"
            
            # ✅ 3. 清理该组的所有 Pending 消息
            PENDING=$(redis-cli XPENDING "$stream" "$group" 2>/dev/null)
            if [ -n "$PENDING" ]; then
                echo "      🧹 清理 Pending 消息..."
                
                # 获取所有 pending 消息 ID
                PENDING_IDS=$(redis-cli XPENDING "$stream" "$group" - + 10000 2>/dev/null | awk '{print $1}')
                
                for msgid in $PENDING_IDS; do
                    # 确认消息（从 Pending 移除）
                    redis-cli XACK "$stream" "$group" "$msgid" >/dev/null 2>&1
                done
                
                echo "      ✅ Pending 消息已清理"
            fi
            
            # ✅ 4. 删除消费者组
            echo "      🗑️  删除消费者组..."
            redis-cli XGROUP DESTROY "$stream" "$group" 2>/dev/null
        done
    fi
    
    # ✅ 5. 清理 Stream 本身
    LENGTH=$(redis-cli XLEN "$stream" 2>/dev/null || echo "0")
    echo "   📊 Stream 长度: $LENGTH"
    
    if [ "$LENGTH" -gt 0 ]; then
        echo "   🗑️  清空 Stream..."
        redis-cli DEL "$stream" >/dev/null 2>&1
        echo "   ✅ Stream 已清空"
    fi
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ 完整清理完成！"
echo ""
echo "📊 清理后的状态："
redis-cli --scan --pattern "*:*" | while read key; do
    TYPE=$(redis-cli TYPE "$key")
    if [ "$TYPE" = "stream" ]; then
        LEN=$(redis-cli XLEN "$key")
        echo "   Stream: $key, 长度: $LEN"
    fi
done