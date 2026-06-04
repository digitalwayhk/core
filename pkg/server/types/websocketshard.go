package types

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// 🆕 分片管理 WebSocket 连接
const shardCount = 128 // 分片数量（支持 10 万连接）

type websocketShard struct {
	clients map[IWebSocket]IRequest
	mu      sync.RWMutex
}

// 🔧 优化注册（使用分片）
func (own *RouterInfo) RegisterWebSocketClient(router IRouter, client IWebSocket, req IRequest) uint64 {
	if router == nil || client == nil || req == nil {
		return 0
	}

	own.ensureWebSocketInit()

	// 🔧 处理私有类型
	if own.PathType == PrivateType {
		id, _ := req.GetUser()
		utils.SetPropertyValue(router, "userid", id)
	}

	hash := getApiHash(router)

	// 🔧 注册路由参数（全局锁，但很快）
	own.Lock()
	if own.rArgs == nil {
		own.rArgs = make(map[uint64]IRouter)
	}
	if own.rHashClients == nil {
		own.rHashClients = make(map[uint64]int)
	}
	own.rHashClients[hash]++
	needRegister := false
	if _, ok := own.rArgs[hash]; !ok {
		own.rArgs[hash] = router
		needRegister = true
	}
	own.Unlock()

	// 🔧 注册客户端（只锁单个分片）
	shard := own.getShard(hash)
	shard.mu.Lock()
	shard.clients[client] = req
	shard.mu.Unlock()

	// 🔧 在锁外调用接口
	if needRegister {
		if iwsr, ok := router.(IWebSocketRouter); ok {
			func() {
				defer func() {
					if err := recover(); err != nil {
						logx.Error("RegisterWebSocket panic:", err)
					}
				}()
				iwsr.RegisterWebSocket(client, req)
			}()
		}
	}

	own.recordWebSocketConnect(hash)

	// Notify cross-node forwarder about new local subscription.
	if f := GetCrossNodeForwarder(); f != nil {
		go f.OnSubscriptionChange(own.Path, hash, true)
	}
	return hash
}

// 🔧 优化注销
func (own *RouterInfo) UnRegisterWebSocketHash(hash uint64, client IWebSocket) {
	if client == nil {
		return
	}

	// 🔧 只锁单个分片
	shard := own.getShard(hash)
	shard.mu.Lock()
	req, existed := shard.clients[client]
	if existed {
		delete(shard.clients, client)
	}
	shard.mu.Unlock()

	if !existed {
		// Client was not in this hash shard — guard against double-unregister.
		return
	}

	// 🔧 检查是否需要清理路由
	var needUnregister bool
	var api IRouter

	own.Lock()
	if own.rHashClients != nil {
		own.rHashClients[hash]--
		if own.rHashClients[hash] < 0 {
			logx.Errorf("rHashClients[%d] underflow detected, resetting to 0", hash)
			own.rHashClients[hash] = 0
		}
		if own.rHashClients[hash] == 0 {
			delete(own.rHashClients, hash)
			api = own.rArgs[hash]
			if api != nil {
				needUnregister = true
			}
			delete(own.rArgs, hash)
		}
	}
	own.Unlock()

	// 🔧 在锁外调用接口
	if needUnregister && api != nil {
		if iwsr, ok := api.(IWebSocketRouter); ok {
			func() {
				defer func() {
					if err := recover(); err != nil {
						logx.Error("UnRegisterWebSocket panic:", err)
					}
				}()
				iwsr.UnRegisterWebSocket(client, req)
			}()
		}
	}

	own.recordWebSocketDisconnect(hash)

	// Notify cross-node forwarder that local subscription may be gone.
	if needUnregister {
		if f := GetCrossNodeForwarder(); f != nil {
			go f.OnSubscriptionChange(own.Path, hash, false)
		}
	}
}

// 🔧 优化广播（使用单例）
func (own *RouterInfo) NoticeWebSocket(message interface{}) {
	if own == nil {
		logx.Errorf("NoticeWebSocket: RouterInfo is nil")
		return
	}

	iwsr, ok := own.instance.(IWebSocketRouterNotice)
	if !ok {
		return
	}

	// 🔧 获取并启动全局系统（单例）
	notifySys := getGlobalNotificationSystem()
	notifySys.Start()

	// 🆕 健康检查
	if !notifySys.IsHealthy() {
		logx.Errorf("通知系统不健康，跳过广播")
		return
	}

	// 🆕 确保分片已初始化
	if len(own.rWebSocketShards) == 0 || own.rWebSocketShards[0] == nil {
		own.ensureWebSocketInit()

		if len(own.rWebSocketShards) == 0 || own.rWebSocketShards[0] == nil {
			logx.Errorf("NoticeWebSocket: 分片初始化失败 for %s", own.Path)
			return
		}
	}

	// 🔧 快速收集 hash 列表（只读锁）
	own.RLock()
	if len(own.rArgs) == 0 {
		own.RUnlock()
		return
	}

	hashes := make([]uint64, 0, len(own.rArgs))
	for hash := range own.rArgs {
		hashes = append(hashes, hash)
	}
	own.RUnlock()

	// 🔧 异步提交任务
	go func() {
		submitted := 0
		dropped := 0

		for _, hash := range hashes {
			own.RLock()
			api, exists := own.rArgs[hash]
			own.RUnlock()

			if !exists || api == nil {
				continue
			}

			job := &noticeJob{
				hash:    hash,
				api:     api,
				message: message,
				iwsr:    iwsr,
				router:  own,
			}

			if job.router == nil || job.iwsr == nil || job.api == nil {
				logx.Errorf("NoticeWebSocket: job 不完整 hash:%d", hash)
				continue
			}

			if notifySys.Submit(job) {
				submitted++
			} else {
				dropped++
			}
		}

		// 🆕 只在有丢弃时才打印
		if dropped > 0 {
			logx.Errorf("%s 提交任务: 成功:%d, 丢弃:%d",
				own.Path, submitted, dropped)
		}

		// Forward notice to peer nodes for the same subscriptions.
		if f := GetCrossNodeForwarder(); f != nil {
			ctx := context.Background()
			for _, hash := range hashes {
				f.ForwardNotice(ctx, own.Path, hash, message)
			}
		}
	}()
}

// 🔧 批量发送
func (own *RouterInfo) sendBatch(clients []IWebSocket, hashStr string, data interface{}) {
	for _, ws := range clients {
		func() {
			defer func() {
				if err := recover(); err != nil {
					own.recordWebSocketError()
				}
			}()

			done := make(chan struct{})
			go func() {
				defer close(done)
				ws.Send(hashStr, own.Path, data)
			}()

			select {
			case <-done:
				own.recordWebSocketMessage(0)
			case <-time.After(3 * time.Second):
				own.recordWebSocketError()
			}
		}()
	}
}

// 🔧 优化清理（并发清理分片）
func (own *RouterInfo) CleanupDeadConnections() {
	var wg sync.WaitGroup
	totalDead := 0
	var mu sync.Mutex

	for i := 0; i < shardCount; i++ {
		wg.Add(1)
		go func(shard *websocketShard) {
			defer wg.Done()

			shard.mu.Lock()
			defer shard.mu.Unlock()

			var deadClients []IWebSocket
			for ws := range shard.clients {
				if ws == nil || ws.IsClosed() {
					deadClients = append(deadClients, ws)
				}
			}

			for _, ws := range deadClients {
				delete(shard.clients, ws)
			}

			mu.Lock()
			totalDead += len(deadClients)
			mu.Unlock()
		}(own.rWebSocketShards[i])
	}

	wg.Wait()

	if totalDead > 0 {
		own.recordDeadConnectionsCleaned(totalDead)
		logx.Infof("清理 %d 个死连接 for %s", totalDead, own.Path)
	}
}

// 🔧 统计活跃连接
func (own *RouterInfo) GetActiveClientCount() int {
	count := 0
	for i := 0; i < shardCount; i++ {
		shard := own.rWebSocketShards[i]
		shard.mu.RLock()
		for ws := range shard.clients {
			if !ws.IsClosed() {
				count++
			}
		}
		shard.mu.RUnlock()
	}
	return count
}

// 🔧 发送到分片客户端（添加完整的防御性检查）
func (own *RouterInfo) sendToHashClients(hash uint64, message, ndata interface{}) {
	// 🆕 第一层防御：检查 RouterInfo 本身
	if own == nil {
		logx.Error("sendToHashClients: RouterInfo is nil")
		return
	}

	// 🆕 第二层防御：检查分片数组是否初始化
	if len(own.rWebSocketShards) == 0 || own.rWebSocketShards[0] == nil {
		logx.Errorf("sendToHashClients: 分片未初始化 for %s, 尝试初始化", own.Path)
		own.ensureWebSocketInit()

		// 再次检查
		if len(own.rWebSocketShards) == 0 || own.rWebSocketShards[0] == nil {
			logx.Errorf("sendToHashClients: 分片初始化失败 for %s", own.Path)
			return
		}
	}

	shard := own.getShard(hash)

	// 🆕 第三层防御：检查分片本身
	if shard == nil {
		logx.Errorf("sendToHashClients: 分片 %d 为 nil for %s", hash%shardCount, own.Path)
		return
	}

	// 🔧 快速收集客户端
	shard.mu.RLock()
	clientCount := len(shard.clients)
	if clientCount == 0 {
		shard.mu.RUnlock()
		return
	}

	clients := make([]IWebSocket, 0, clientCount)
	for ws := range shard.clients {
		if ws != nil && !ws.IsClosed() {
			clients = append(clients, ws)
		}
	}
	shard.mu.RUnlock()

	if len(clients) == 0 {
		return
	}
	// logx.Infow("发送WebSocket消息",
	// 	logx.Field("clients", len(clients)),
	// 	logx.Field("hash", hash),
	// 	logx.Field("path", own.Path),
	// 	logx.Field("data", utils.PrintObj(message)),
	// )
	// 🔧 批量发送
	own.recordWebSocketBroadcast(len(clients))
	hashStr := strconv.FormatUint(hash, 10)

	const batchSize = 500
	for i := 0; i < len(clients); i += batchSize {
		end := i + batchSize
		if end > len(clients) {
			end = len(clients)
		}

		batch := clients[i:end]
		go own.sendBatch(batch, hashStr, ndata)
	}
	logx.Infof("已启动 %d 个批次发送任务 for %s", (len(clients)+batchSize-1)/batchSize, own.Path)
}

// 🔧 优化 getShard（添加边界检查）
func (own *RouterInfo) getShard(hash uint64) *websocketShard {
	if own == nil || len(own.rWebSocketShards) == 0 {
		return nil
	}

	index := hash % shardCount
	if int(index) >= len(own.rWebSocketShards) {
		logx.Errorf("getShard: 索引越界 hash:%d, index:%d, len:%d",
			hash, index, len(own.rWebSocketShards))
		return nil
	}

	return own.rWebSocketShards[index]
}

// 🔧 确保分片初始化是线程安全的
func (own *RouterInfo) ensureWebSocketInit() {
	own.once.Do(func() {
		// 🔧 1. 先初始化分片
		if len(own.rWebSocketShards) == 0 || own.rWebSocketShards[0] == nil {
			own.initShards()
		}

		// 🔧 2. 再初始化统计（依赖分片）
		if own.stats == nil {
			own.initStats()
		}

		// 🔧 3. 注册到全局清理
		websocketcleanupOnce.Do(func() {
			logx.Info("🚀 启动全局WebSocket清理任务")
			StartPeriodicCleanup()
		})

		key := own.ServiceName + ":" + own.Path
		if keyhash, ok := own.instance.(IRouterHashKey); ok {
			hashStr := strconv.FormatUint(keyhash.GetHashKey(), 10)
			key = key + ":" + hashStr
		}

		if _, loaded := clearMap.LoadOrStore(key, own); !loaded {
			logx.Infof("📝 注册WebSocket路由: %s", key)
		}
	})
}

// 🔧 修复 initShards，添加日志
func (own *RouterInfo) initShards() {
	if len(own.rWebSocketShards) > 0 && own.rWebSocketShards[0] != nil {
		// 已经初始化过
		return
	}

	logx.Infof("初始化 %d 个分片 for %s", shardCount, own.Path)

	for i := 0; i < shardCount; i++ {
		own.rWebSocketShards[i] = &websocketShard{
			clients: make(map[IWebSocket]IRequest),
		}
	}
	own.rHashClients = make(map[uint64]int)

	logx.Infof("✅ 分片初始化完成 for %s", own.Path)
}

// ExecuteLocalNotice delivers a forwarded cross-node notice to local subscribers
// for the given hash bucket. This is called by the manage relay endpoint when a
// peer node forwards a NoticeWebSocket message to this node.
func (own *RouterInfo) ExecuteLocalNotice(hash uint64, message interface{}) {
	if own == nil {
		return
	}
	iwsr, ok := own.instance.(IWebSocketRouterNotice)
	if !ok {
		return
	}
	own.RLock()
	api, exists := own.rArgs[hash]
	own.RUnlock()
	if !exists || api == nil {
		return
	}

	notifySys := getGlobalNotificationSystem()
	notifySys.Start()
	if !notifySys.IsHealthy() {
		return
	}

	notifySys.Submit(&noticeJob{
		hash:    hash,
		api:     api,
		message: message,
		iwsr:    iwsr,
		router:  own,
	})
}

// GetSubscribedHashes returns the set of active subscription hashes (routerArg hashes)
// for this RouterInfo. Used by the cross-node forwarder to track local subscriptions.
func (own *RouterInfo) GetSubscribedHashes() []uint64 {
	own.RLock()
	defer own.RUnlock()
	hashes := make([]uint64, 0, len(own.rArgs))
	for h := range own.rArgs {
		hashes = append(hashes, h)
	}
	return hashes
}
