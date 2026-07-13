package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/routecache"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/yitter/idgenerator-go/idgen"
	"github.com/zeromicro/go-zero/core/logx"
)

// processLocalRegistry is a shared in-memory cluster registry for all ServiceContexts
// within the same process. This enables intra-process MachineID conflict detection.
var processLocalRegistry = cluster.NewLocalProvider(
	config.DefaultClusterHeartbeatTimeout,
	config.DefaultClusterSuspectTimeout,
	config.DefaultClusterInstanceReuseCooldown,
)

func init() {
	processLocalRegistry.Start()
}

type ServiceContext struct {
	Config             *config.ServerConfig
	Service            *types.Service
	snow               idgen.ISnowWorker
	Router             *ServiceRouter
	isStart            atomic.Bool
	terminated         bool
	shutdownDone       chan struct{}
	shutdownOnce       sync.Once
	lifecycleMu        sync.Mutex
	lifecycleOpOnce    sync.Once
	lifecycleOp        chan struct{} // 串行化启停和 Provider 切换，但不在 Provider 调用期间持有状态锁。
	Pid                int
	Hub                interface{} `json:"-"`
	StateChan          chan bool   `json:"-"`
	serverOption       *types.ServerOption
	serverOptionMu     sync.RWMutex
	TransportSelector  transport.TransportSelector    `json:"-"`
	MQManager          *mq.MQManager                  `json:"-"`
	EventStream        *event.Stream                  `json:"-"`
	EventBridge        *event.MQBridge                `json:"-"`
	ServiceEventBridge *event.ServiceEventBridge      `json:"-"`
	RouteWebSocketHub  *types.RouteWebSocketHub       `json:"-"`
	RouteCacheManager  *routecache.Manager            `json:"-"`
	ClusterProvider    cluster.DiscoveryProvider      `json:"-"`
	ClusterSwitcher    cluster.ProviderSwitcher       `json:"-"`
	membership         *cluster.MembershipManager     `json:"-"`
	CrossNodeBroker    *cluster.CrossNodeNoticeBroker `json:"-"`
	nodeID             string
	configFingerprint  string
}

func (own *ServiceContext) beginLifecycleOperation() {
	own.lifecycleOpOnce.Do(func() {
		own.lifecycleOp = make(chan struct{}, 1)
		own.lifecycleOp <- struct{}{}
	})
	<-own.lifecycleOp
}

func (own *ServiceContext) endLifecycleOperation() {
	own.lifecycleOp <- struct{}{}
}

func (own *ServiceContext) shutdownWaiter() (<-chan struct{}, bool) {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	if !own.terminated {
		return nil, false
	}
	if own.shutdownDone == nil {
		own.shutdownDone = make(chan struct{})
	}
	return own.shutdownDone, true
}

func (own *ServiceContext) completeShutdown() {
	own.lifecycleMu.Lock()
	if !own.terminated {
		own.lifecycleMu.Unlock()
		return
	}
	if own.shutdownDone == nil {
		own.shutdownDone = make(chan struct{})
	}
	done := own.shutdownDone
	own.lifecycleMu.Unlock()
	own.shutdownOnce.Do(func() { close(done) })
}

func (own *ServiceContext) GetServerOption() *types.ServerOption {
	if own == nil {
		return nil
	}
	own.serverOptionMu.RLock()
	option := own.serverOption.Clone()
	own.serverOptionMu.RUnlock()
	if option != nil && own.Config != nil {
		option.RemoteAccessManageAPI = own.Config.RemoteAccessManageAPI
	}
	return option
}
func (own *ServiceContext) SetServerOption(so *types.ServerOption) {
	own.serverOptionMu.Lock()
	own.serverOption = so.Clone()
	own.serverOptionMu.Unlock()
}

// EnableEventBridge wires an in-process event.Stream to the MQManager so that
// event.Envelope values can be published and consumed via the MQ provider.
// It is called automatically during NewServiceContext when MQ.Usage contains
// "event-stream", and is also exposed for use in tests.
func (own *ServiceContext) EnableEventBridge() {
	if own.MQManager == nil {
		return
	}
	if own.EventStream == nil {
		own.EventStream = event.NewStream()
	}
	if own.ServiceEventBridge == nil {
		own.ServiceEventBridge = event.NewServiceEventBridge(own.EventStream, event.ServiceEventBridgeOptions{})
	}
	own.EventBridge = event.NewMQBridge(own.EventStream, own.MQManager)
	own.ServiceEventBridge.SetExternalPublisher(own.EventBridge)
}

// containsUsage reports whether usage slice contains the given value.
func containsUsage(usage []string, value string) bool {
	for _, u := range usage {
		if u == value {
			return true
		}
	}
	return false
}
func (own *ServiceContext) getStatsManager() *StatsManager {
	return NewStatsManager(own.Service.Name, own.Router.GetRouters())
}

// 🆕 GetAllRouterStats 获取所有路由统计（支持过滤和排序）
func (own *ServiceContext) GetAllRouterStats(
	filterTypes []types.ApiType,
	sortBy SortField,
	order SortOrder,
) *AggregatedStats {
	manager := own.getStatsManager()
	return manager.GetAllStats(filterTypes, sortBy, order)
}

// 🆕 GetPublicRouterStats 获取公共路由统计（排序）
func (own *ServiceContext) GetPublicRouterStats(
	sortBy SortField,
	order SortOrder,
) *AggregatedStats {
	return own.GetAllRouterStats(
		[]types.ApiType{types.PublicType},
		sortBy,
		order,
	)
}

// 🆕 GetPrivateRouterStats 获取私有路由统计（排序）
func (own *ServiceContext) GetPrivateRouterStats(
	sortBy SortField,
	order SortOrder,
) *AggregatedStats {
	return own.GetAllRouterStats(
		[]types.ApiType{types.PrivateType},
		sortBy,
		order,
	)
}

// 🆕 GetTopRouters 获取排名前N的路由
func (own *ServiceContext) GetTopRouters(
	n int,
	filterTypes []types.ApiType,
	sortBy SortField,
) []*types.RouterStatsSnapshot {
	manager := own.getStatsManager()
	return manager.GetTopN(n, filterTypes, sortBy)
}

// 🆕 PrintRouterStats 打印路由统计
func (own *ServiceContext) PrintRouterStats(
	filterTypes []types.ApiType,
	sortBy SortField,
) {
	manager := own.getStatsManager()
	summary := manager.GetSummary(filterTypes)
	logx.Info(summary)

	// 打印 Top 10
	manager.PrintTopStats(10, filterTypes, sortBy)
}

// 🆕 GetStatsJSON 获取JSON格式统计
func (own *ServiceContext) GetStatsJSON(
	filterTypes []types.ApiType,
	sortBy SortField,
	order SortOrder,
) string {
	manager := own.getStatsManager()
	return manager.GetStatsJSON(filterTypes, sortBy, order)
}

// 获取响应最慢的10个路由
func (own *ServiceContext) GetSlowestRoutersJSON(
	sortBy SortField,
) string {
	manager := own.getStatsManager()
	filterTypes := []types.ApiType{types.PrivateType, types.PublicType}
	topRouters := manager.GetTopN(10, filterTypes, sortBy)
	data, err := json.MarshalIndent(topRouters, "", "  ")
	if err != nil {
		logx.Error("Failed to marshal slowest routers JSON:", err)
		return ""
	}
	return string(data)
}

const DEFAULTPORT = 8080
const DEFAULTSOCKETPORT = 0

type contextInitialization struct {
	ready      chan struct{}
	result     *ServiceContext
	panicValue interface{}
	panicked   bool
}

type serviceContextRegistry struct {
	mu                   sync.RWMutex
	contexts             map[string]*ServiceContext
	initializing         map[string]*contextInitialization
	nextDefaultSequence  int
	freeDefaultSequences []int
}

func newServiceContextRegistry() *serviceContextRegistry {
	return &serviceContextRegistry{
		contexts:     make(map[string]*ServiceContext),
		initializing: make(map[string]*contextInitialization),
	}
}

func (r *serviceContextRegistry) get(name string) *ServiceContext {
	r.mu.RLock()
	sc := r.contexts[name]
	r.mu.RUnlock()
	if sc != nil {
		if _, terminated := sc.shutdownWaiter(); terminated {
			return nil
		}
	}
	return sc
}

func (r *serviceContextRegistry) snapshot() map[string]*ServiceContext {
	r.mu.RLock()
	defer r.mu.RUnlock()

	contexts := make(map[string]*ServiceContext, len(r.contexts))
	for name, sc := range r.contexts {
		if _, terminated := sc.shutdownWaiter(); !terminated {
			contexts[name] = sc
		}
	}
	return contexts
}

func (r *serviceContextRegistry) getOrInitialize(
	name string,
	reserveDefaultSequence bool,
	requestedFingerprint string,
	initialize func(sequence int) *ServiceContext,
) *ServiceContext {
	for {
		r.mu.Lock()
		if sc := r.contexts[name]; sc != nil {
			if done, terminated := sc.shutdownWaiter(); terminated {
				r.mu.Unlock()
				<-done
				continue
			}
			r.mu.Unlock()
			assertServiceContextConfig(name, requestedFingerprint, sc)
			return sc
		}
		if entry := r.initializing[name]; entry != nil {
			r.mu.Unlock()
			<-entry.ready
			if entry.panicked {
				panic(entry.panicValue)
			}
			continue
		}
		break
	}

	sequence := -1
	if reserveDefaultSequence {
		sequence = r.reserveDefaultSequence()
	}
	entry := &contextInitialization{ready: make(chan struct{})}
	r.initializing[name] = entry
	r.mu.Unlock()

	var result *ServiceContext
	func() {
		defer func() {
			if value := recover(); value != nil {
				entry.panicValue = value
				entry.panicked = true
			}
		}()
		result = initialize(sequence)
	}()

	r.mu.Lock()
	entry.result = result
	if !entry.panicked && result != nil {
		r.contexts[name] = result
	} else if reserveDefaultSequence {
		r.freeDefaultSequences = append(r.freeDefaultSequences, sequence)
	}
	delete(r.initializing, name)
	close(entry.ready)
	r.mu.Unlock()

	if entry.panicked {
		panic(entry.panicValue)
	}
	return result
}

func assertServiceContextConfig(name, requestedFingerprint string, sc *ServiceContext) {
	if sc == nil || requestedFingerprint == "" || sc.configFingerprint == "" {
		return
	}
	if requestedFingerprint != sc.configFingerprint {
		panic(fmt.Sprintf("service context config conflict: service=%s", name))
	}
}

func (r *serviceContextRegistry) remove(name string, expected *ServiceContext) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contexts[name] != expected {
		return false
	}
	delete(r.contexts, name)
	return true
}

// reserveDefaultSequence 必须在持有 r.mu 时调用。
func (r *serviceContextRegistry) reserveDefaultSequence() int {
	for len(r.freeDefaultSequences) > 0 {
		last := len(r.freeDefaultSequences) - 1
		sequence := r.freeDefaultSequences[last]
		r.freeDefaultSequences = r.freeDefaultSequences[:last]
		if sequence >= len(r.contexts) {
			return sequence
		}
	}
	if r.nextDefaultSequence < len(r.contexts) {
		r.nextDefaultSequence = len(r.contexts)
	}
	sequence := r.nextDefaultSequence
	r.nextDefaultSequence++
	return sequence
}

var contextRegistry = newServiceContextRegistry()

var testResultMu sync.RWMutex

// Deprecated: 为保持源码兼容暂时保留；框架内部请使用 SetTestResult 和 GetTestResult。
// 外部直接写入此 map 无法受内部锁保护，仍可能产生并发竞态。
var TestResult = make(map[string]interface{})

func SetTestResult(path string, result interface{}) {
	testResultMu.Lock()
	defer testResultMu.Unlock()
	TestResult[path] = result
}

func GetTestResult(path string) interface{} {
	testResultMu.RLock()
	defer testResultMu.RUnlock()
	return TestResult[path]
}

func NewServiceContext(service types.IService) *ServiceContext {
	name := strings.ToLower(service.ServiceName())
	con := config.ReadConfig(name)
	requestedFingerprint := ""
	if con != nil {
		normalized, fingerprint, err := normalizeServerConfig(con)
		if err != nil {
			panic(fmt.Sprintf("config validation failed: %v", err))
		}
		con = normalized
		requestedFingerprint = fingerprint
	}
	return contextRegistry.getOrInitialize(name, true, requestedFingerprint, func(sequence int) *ServiceContext {
		sc := &ServiceContext{}
		sc.StateChan = make(chan bool, 1)
		sc.Service = initService(service, sc)
		if con == nil {
			port := DEFAULTPORT + sequence
			con = config.NewServiceDefaultConfig(name, port)
			con.DataCenterID = uint(sequence) + 1
			con.MachineID = 1
			con.SocketPort = DEFAULTSOCKETPORT + sequence
			con.AttachServices = make(map[string]*config.AttachAddress)
			for _, as := range sc.Service.AttachService {
				con.SetAttachService(as.ServiceName, "", 0, 0)
			}
			if err := con.Save(); err != nil {
				panic(err)
			}
		} else {
			for _, as := range sc.Service.AttachService {
				if cas, ok := con.AttachServices[as.ServiceName]; ok {
					as.Address = cas.Address
					as.Port = cas.Port
				}
			}
		}
		sc.Config = con
		fingerprint, err := serverConfigFingerprint(con)
		if err != nil {
			panic(err)
		}
		sc.configFingerprint = fingerprint
		initServiceContextPost(sc, service, con)
		return sc
	})
}

// NewServiceContextWithConfig creates a ServiceContext using the provided
// config directly, bypassing file-based config loading. Intended for testing
// and programmatic service bootstrap where the caller manages configuration.
func NewServiceContextWithConfig(service types.IService, con *config.ServerConfig) *ServiceContext {
	name := strings.ToLower(service.ServiceName())
	if con == nil {
		panic("config validation failed: config is nil")
	}
	normalized, fingerprint, err := normalizeServerConfig(con)
	if err != nil {
		panic(fmt.Sprintf("config validation failed: %v", err))
	}
	con = normalized
	return contextRegistry.getOrInitialize(name, false, fingerprint, func(_ int) *ServiceContext {
		sc := &ServiceContext{}
		sc.StateChan = make(chan bool, 1)
		sc.Service = initService(service, sc)
		sc.Config = con
		sc.configFingerprint = fingerprint
		initServiceContextPost(sc, service, con)
		return sc
	})
}

// initServiceContextPost performs the post-config initialisation shared by
// NewServiceContext and NewServiceContextWithConfig: MachineID claiming,
// cluster/transport/MQ provider setup, Snowflake, and router wiring.
func initServiceContextPost(sc *ServiceContext, service types.IService, con *config.ServerConfig) {
	sc.EventStream = event.NewStream()
	sc.ServiceEventBridge = event.NewServiceEventBridge(sc.EventStream, event.ServiceEventBridgeOptions{})
	sc.RouteWebSocketHub = types.NewRouteWebSocketHub(sc.Service.Name, sc.ServiceEventBridge)

	// Phase 4: claim a unique MachineID in the process-local registry before
	// initialising Snowflake, preventing ID collisions between services in the
	// same process or between hot-reload replicas.
	if con.Cluster.Mode != "off" {
		machineID, err := claimMachineID(con, sc.Service.Name)
		if err != nil {
			logx.Errorf("cluster: MachineID claim failed (%v), proceeding with config value", err)
		} else {
			con.MachineID = uint(machineID)
		}
	}

	if err := initCluster(sc); err != nil {
		if con.Cluster.Mode == "on" {
			panic(fmt.Sprintf("cluster: required provider init failed (mode=on): %v", err))
		}
		logx.Infow("cluster_degraded",
			logx.Field("service", sc.Service.Name),
			logx.Field("error", err),
		)
	}
	if sel, selErr := transport.BuildSelector(con.Transport); selErr != nil {
		// Any error from BuildSelector means the user explicitly configured a
		// transport protocol that cannot be built (e.g. quic, mq not yet implemented).
		// This is a hard misconfiguration — prevent silent fallback to legacy HTTP.
		panic(fmt.Sprintf("transport: init failed: %v", selErr))
	} else if sel != nil {
		sc.TransportSelector = sel
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		mgr, mqErr := mq.BuildManager(ctx, &con.MQ)
		cancel()
		if mqErr != nil {
			panic(fmt.Sprintf("mq: provider init failed (mode=%s): %v", con.MQ.Mode, mqErr))
		} else {
			sc.MQManager = mgr
			// Wire MQ-backed event stream when usage includes "event-stream".
			if mgr != nil && containsUsage(con.MQ.Usage, "event-stream") {
				sc.EnableEventBridge()
			}
		}
	}

	// shared 缓存必须等 MQ/EventBridge 外部适配器装配完成后再初始化，确保
	// Redis 事实缓存与跨节点失效订阅同时就绪，不允许只启动本地层。
	cacheManager, cacheErr := routecache.NewManager(
		sc.Service.Name,
		con.RouteCache,
		routecache.WithInvalidationBridge(sc.ServiceEventBridge),
	)
	if cacheErr != nil {
		if sc.MQManager != nil {
			_ = sc.MQManager.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = sc.RouteWebSocketHub.Close(ctx)
		_ = sc.ServiceEventBridge.Close(ctx)
		cancel()
		panic(fmt.Sprintf("route cache: init failed: %v", cacheErr))
	}
	sc.RouteCacheManager = cacheManager

	sc.snow = utils.NewAlgorithmSnowFlake(con.MachineID, con.DataCenterID)
	sc.Router = NewServiceRouter(sc, service)
}
func initService(iser types.IService, sc *ServiceContext) *types.Service {
	service := &types.Service{
		Name:             strings.ToLower(iser.ServiceName()),
		Routers:          iser.Routers(),
		SubscribeRouters: iser.SubscribeRouters(),
		AttachService:    make(map[string]*types.ServiceAttach),
		Instance:         iser,
	}
	for _, sr := range service.SubscribeRouters {
		as := addAttachService(service, sr.ServiceName)
		as.ObserverRouters[sr.Topic] = sr
	}
	req := &InitRequest{}
	for _, cs := range service.Routers {
		safedo(cs, req)
	}
	if req.CallRouters != nil {
		for path, cr := range req.CallRouters {
			cinfo := cr.RouterInfo()
			sname := cinfo.GetServiceName()
			as := addAttachService(service, sname)
			if as.CallRouters == nil {
				as.CallRouters = make(map[string]types.IRouter)
			}
			as.CallRouters[path] = cr
		}
	}
	return service
}
func addAttachService(service *types.Service, tragetServiceName string) *types.ServiceAttach {
	if _, ok := service.AttachService[tragetServiceName]; !ok {
		service.AttachService[tragetServiceName] = &types.ServiceAttach{
			ServiceName:     tragetServiceName,
			ObserverRouters: make(map[string]*types.ObserveArgs),
		}
	}
	return service.AttachService[tragetServiceName]
}
func safedo(cs types.IRouter, req types.IRequest) {
	defer func() {
		if err := recover(); err != nil {
			//logx.Error(err)
			// info := cs.RouterInfo()
			// fmt.Println(fmt.Sprintf("服务%s的路由%s发生异常:", info.ServiceName, info.Path), err)
		}
	}()
	// serviceName := req.ServiceName()
	// path := req.GetPath()
	// err := cs.Validation(req)
	// if err != nil {
	// 	logx.Error(fmt.Sprintf("服务%s的路由%s验证失败:%s", serviceName, path, err.Error()))
	// }
	// data, err := cs.Do(req)
	// if err != nil {
	// 	logx.Error(fmt.Sprintf("服务%s的路由%s执行失败:%s", serviceName, path, err.Error()))
	// }
	info := cs.RouterInfo()
	SetTestResult(info.GetPath(), nil)
}
func GetContext(name string) *ServiceContext {
	if name == "" {
		return nil
	}
	return contextRegistry.get(name)
}
func GetContexts() map[string]*ServiceContext {
	return contextRegistry.snapshot()
}
func (own *ServiceContext) NewID() uint {
	return uint(own.snow.NextId())
}
func (own *ServiceContext) SetPid(pid int) {
	own.Pid = pid
}
func (own *ServiceContext) SetRunState(state bool) {
	own.beginLifecycleOperation()
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	if own.terminated {
		own.lifecycleMu.Unlock()
		return
	}
	if own.isStart.Load() == state {
		if state || own.MQManager == nil {
			own.lifecycleMu.Unlock()
			return
		}
	} else {
		own.isStart.Store(state)
	}
	if !state {
		own.terminated = true
		if own.shutdownDone == nil {
			own.shutdownDone = make(chan struct{})
		}
	}
	provider := own.ClusterProvider
	membership := own.membership
	broker := own.CrossNodeBroker
	mqManager := own.MQManager
	serviceEventBridge := own.ServiceEventBridge
	routeWebSocketHub := own.RouteWebSocketHub
	routeCacheManager := own.RouteCacheManager
	if !state {
		own.membership = nil
		own.CrossNodeBroker = nil
		own.MQManager = nil
		own.EventBridge = nil
		own.ServiceEventBridge = nil
		own.RouteWebSocketHub = nil
		own.RouteCacheManager = nil
		own.EventStream = nil
	}
	own.lifecycleMu.Unlock()
	if !state {
		defer func() {
			contextRegistry.remove(own.Service.Name, own)
			own.completeShutdown()
		}()
	}

	if state {
		nodeID, node, interval := own.clusterMembershipConfig()
		if provider != nil && membership == nil {
			membership = own.startMembership(provider, node, interval)
		}
		if provider != nil && broker == nil {
			broker = cluster.NewCrossNodeNoticeBroker(provider, own.Service.Name, nodeID)
			if own.TransportSelector != nil {
				broker.SetSender(own.makeCrossNodeSender())
			}
			types.SetCrossNodeForwarderForService(own.Service.Name, broker)
		}
		own.lifecycleMu.Lock()
		own.nodeID = nodeID
		own.membership = membership
		own.CrossNodeBroker = broker
		own.lifecycleMu.Unlock()
	} else {
		if routeWebSocketHub != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := routeWebSocketHub.Close(ctx); err != nil {
				logx.Errorw("route_websocket_hub_close_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("error", err),
				)
			}
			cancel()
		}
		if routeCacheManager != nil {
			routeCacheManager.Close()
		}
		if serviceEventBridge != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := serviceEventBridge.Close(ctx); err != nil {
				logx.Errorw("service_event_bridge_close_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("error", err),
				)
			}
			cancel()
		}
		if broker != nil {
			types.ClearCrossNodeForwarderForService(own.Service.Name, broker)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			broker.DrainAndStop(ctx)
			cancel()
		}
		if membership != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			membership.Stop(ctx)
			cancel()
		}
		if mqManager != nil {
			if err := mqManager.Close(); err != nil {
				logx.Errorf("mq: close failed: %v", err)
			}
		}
	}

	// 🔧 非阻塞发送，避免死锁
	select {
	case own.StateChan <- state:
		// 发送成功
	default:
		// 通道满了，在当前架构下这不是问题
		logx.Debugf("StateChan已满，跳过状态通知: %s", own.Service.Name)
	}
}

// SyncProviderAfterSwitch updates ClusterProvider to match the switcher's
// current provider and, if the service is already running, restarts membership
// and the CrossNode broker with the new provider.
func (own *ServiceContext) SyncProviderAfterSwitch() error {
	own.beginLifecycleOperation()
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	switcher := own.ClusterSwitcher
	oldProvider := own.ClusterProvider
	own.lifecycleMu.Unlock()
	if switcher == nil {
		return nil
	}
	newProvider := switcher.Current()
	if newProvider == oldProvider {
		return nil
	}

	own.lifecycleMu.Lock()
	own.ClusterProvider = newProvider
	running := own.isStart.Load()
	membership := own.membership
	broker := own.CrossNodeBroker
	if running {
		own.membership = nil
		own.CrossNodeBroker = nil
	}
	own.lifecycleMu.Unlock()
	if !running {
		return nil
	}

	if membership != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		membership.Stop(stopCtx)
		cancel()
	}
	if broker != nil {
		types.ClearCrossNodeForwarderForService(own.Service.Name, broker)
		drainCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		broker.DrainAndStop(drainCtx)
		cancel()
	}

	nodeID, node, interval := own.clusterMembershipConfig()
	var newMembership *cluster.MembershipManager
	var newBroker *cluster.CrossNodeNoticeBroker
	if newProvider != nil {
		newMembership = own.startMembership(newProvider, node, interval)
		newBroker = cluster.NewCrossNodeNoticeBroker(newProvider, own.Service.Name, nodeID)
		if own.TransportSelector != nil {
			newBroker.SetSender(own.makeCrossNodeSender())
		}
		types.SetCrossNodeForwarderForService(own.Service.Name, newBroker)
	}

	own.lifecycleMu.Lock()
	own.nodeID = nodeID
	own.membership = newMembership
	own.CrossNodeBroker = newBroker
	own.lifecycleMu.Unlock()
	return nil
}

func (own *ServiceContext) clusterMembershipConfig() (string, *cluster.NodeInfo, time.Duration) {
	nodeID := fmt.Sprintf("%s-%d-%d", own.Service.Name,
		own.Config.DataCenterID, own.Config.MachineID)
	node := &cluster.NodeInfo{
		ID:           nodeID,
		ServiceName:  own.Service.Name,
		DataCenterID: int64(own.Config.DataCenterID),
		MachineID:    int64(own.Config.MachineID),
		Address:      own.Config.RunIp,
		Port:         own.Config.Port,
		SocketPort:   own.Config.SocketPort,
		Weight:       1,
	}
	interval := own.Config.Cluster.HeartbeatInterval
	if interval == 0 {
		interval = 3 * time.Second
	}
	return nodeID, node, interval
}

func (own *ServiceContext) startMembership(
	provider cluster.DiscoveryProvider,
	node *cluster.NodeInfo,
	interval time.Duration,
) *cluster.MembershipManager {
	if err := provider.Register(context.Background(), node); err != nil {
		logx.Errorf("cluster: register node %s: %v", node.ID, err)
		return nil
	}
	membership := cluster.NewMembershipManager(provider, node.ID, interval)
	membership.Start(context.Background())
	return membership
}

func (own *ServiceContext) IsRun() bool {
	return own.isStart.Load()
}
func (own *ServiceContext) SetHttpServer(server types.IRunServer) {
	own.Service.HttpServer = server
}
func (own *ServiceContext) SetSocketServer(server types.IRunServer) {
	own.Service.AddInternalServer(server)
}
func (own *ServiceContext) GetServers() []types.IRunServer {
	items := make([]types.IRunServer, 0)
	items = append(items, own.Service.HttpServer)
	items = append(items, own.Service.GetInternalServers()...)
	return items
}
func (own *ServiceContext) SetAttachServiceAddress(name string) error {
	if cas, ok := own.Config.AttachServices[name]; ok {
		if as, ok := own.Service.AttachService[name]; ok {
			as.Address = cas.Address
			as.Port = cas.Port
			// if cas.SocketPort == 0 {
			// 	csc := own.GetServerConfig(as.Address, as.Port)
			// 	if csc != nil {
			// 		as.SocketPort = csc.SocketPort
			// 		cas.SocketPort = csc.SocketPort
			// 		cas.Address = csc.RunIp
			// 		as.Address = csc.RunIp
			// 		own.Config.Save()
			// 	}
			// }
			as.SocketPort = cas.SocketPort
			as.IsAttach = false
			for _, sr := range as.ObserverRouters {
				sr.IsOk = false
			}
		}
	}
	return nil
}
func (own *ServiceContext) GetServerConfig(address string, port int) *config.ServerConfig {
	payload := &types.PayLoad{
		TraceID:       "",
		TargetAddress: address,
		TargetPort:    port,
		SourcePath:    "",
		TargetService: "config",
		TargetPath:    "/api/servermanage/queryconfig",
	}
	values, err := own.Service.CallService(payload)
	if err != nil {
		logx.Error(err)
		return nil
	}
	res := &Response{}
	json.Unmarshal(values, res)
	csc := &config.ServerConfig{}
	res.GetData(csc)
	return csc
}
func (own *ServiceContext) RegisterObserveSub(oa *types.ObserveArgs, info *types.TargetInfo) error {
	as := addAttachService(own.Service, oa.ServiceName)
	if _, ok := as.ObserverRouters[oa.Topic]; !ok {
		ok, err := own.observeCall(oa, info)
		if err != nil {
			return err
		}
		as.IsAttach = ok
		oa.IsOk = ok
		as.ObserverRouters[oa.Topic] = oa
	}
	return nil
}
func (own *ServiceContext) RegisterObserve(observe types.IRouter) error {
	info := observe.RouterInfo()
	for _, as := range own.Service.AttachService {
		if as.Address == "" || as.Port == 0 {
			continue
		}
		for _, oa := range as.ObserverRouters {
			ti := &types.TargetInfo{}
			ti.TargetAddress = as.Address
			ti.TargetPort = as.Port
			ti.TargetService = as.ServiceName
			ti.TargetPath = info.GetPath()
			ti.TargetSocketPort = as.SocketPort
			ok, err := own.observeCall(oa, ti)
			if err != nil {
				return err
			}
			oa.IsOk = ok
			as.IsAttach = ok
		}
	}
	return nil
}

var observeMap map[string]*types.PayLoad = make(map[string]*types.PayLoad)
var obseLock sync.RWMutex

func addObserveMap(own *ServiceContext, payload *types.PayLoad) {
	obseLock.Lock()
	defer obseLock.Unlock()
	observeMap[own.Service.Name] = payload
}
func removeObserveMap(own *ServiceContext, payload *types.PayLoad) {
	obseLock.Lock()
	defer obseLock.Unlock()
	for k, v := range observeMap {
		sv := v.Instance.(*types.ObserveArgs)
		tv := payload.Instance.(*types.ObserveArgs)
		if own.Service.Name == k && sv.Topic == tv.Topic {
			delete(observeMap, k)
		}
	}
}

var runobserve sync.Once

func runobservemap() {
	for {
		time.Sleep(time.Second * 60)
		obseLock.Lock()
		for k, v := range observeMap {
			own := GetContext(k)
			if own == nil {
				continue
			}
			values, err := own.Service.CallService(v)
			if err != nil {
				logx.Errorw("observe_call_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("target_service", v.TargetService),
					logx.Field("target_route", v.TargetPath),
					logx.Field("error", err),
				)
			}
			res := &Response{}
			if err := json.Unmarshal(values, res); err != nil {
				logx.Errorw("observe_response_decode_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("target_service", v.TargetService),
					logx.Field("error", err),
				)
				continue
			}
			if !res.Success {
				logx.Errorw("observe_response_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("target_service", v.TargetService),
					logx.Field("target_route", v.TargetPath),
				)
			} else {
				logx.Debugw("observe_call_completed",
					logx.Field("service", own.Service.Name),
					logx.Field("target_service", v.TargetService),
					logx.Field("target_route", v.TargetPath),
				)
			}
		}
		obseLock.Unlock()
	}
}
func (own *ServiceContext) observeCall(oa *types.ObserveArgs, info *types.TargetInfo) (bool, error) {
	if oa.ServiceName == "" || oa.Topic == "" {
		return false, errors.New("observeCall ServiceName or Topic is empty")
	}
	if info.TargetAddress == "" || info.TargetPort == 0 || info.TargetService == "" || info.TargetPath == "" {
		return false, errors.New("observeCall TargetAddress or TargetPort or TargetService or TargetPath is empty")
	}
	oa.OwnAddress = own.Config.RunIp
	oa.OwnProt = own.Config.Port
	oa.OwnSocketProt = own.Config.SocketPort
	oa.ReceiveService = own.Service.Name
	payload := &types.PayLoad{
		TraceID:          "1",
		SourceAddress:    oa.OwnAddress,
		SourceService:    oa.ReceiveService,
		TargetAddress:    info.TargetAddress,
		TargetService:    info.TargetService,
		TargetPort:       info.TargetPort,
		TargetSocketPort: info.TargetSocketPort,
		SourcePath:       "",
		TargetPath:       info.TargetPath,
		UserId:           "",
		ClientIP:         oa.OwnAddress,
		Auth:             false,
		Instance:         oa,
	}
	values, err := own.Service.CallService(payload)
	if err != nil {
		oa.Error = err
		return false, err
	}
	res := &Response{}
	json.Unmarshal(values, res)
	if !res.Success {
		oa.Error = errors.New(res.ErrorMessage)
		return false, oa.Error
	} else {
		runobserve.Do(func() {
			go runobservemap()
		})
		if oa.IsUnSub {
			removeObserveMap(own, payload)
		} else {
			addObserveMap(own, payload)
		}
	}
	return true, nil
}

func SendNotify(notify types.IRouter, args *types.NotifyArgs) error {
	ctx := GetContext(args.SendService)
	if ctx == nil {
		return errors.New(args.SendService + "service not found")
	}
	info := notify.RouterInfo()
	payload := &types.PayLoad{
		TraceID:          args.TraceID,
		SourceAddress:    ctx.Config.RunIp,
		SourceService:    args.SendService,
		TargetAddress:    args.ReceiveAddress,
		TargetService:    args.ReceiveService,
		TargetPort:       args.ReceiveProt,
		TargetSocketPort: args.ReceiveSocketProt,
		SourcePath:       args.Topic,
		TargetPath:       info.GetPath(),
		ClientIP:         ctx.Config.RunIp,
		Auth:             false,
		Instance:         args,
	}
	values, err := ctx.Service.CallService(payload)
	if err != nil {
		return err
	}
	res := &Response{}
	json.Unmarshal(values, res)
	if !res.Success {
		return res.GetError()
	}
	return nil
}
func (own *ServiceContext) CallTargetService(traceid string, router types.IRouter, info *types.TargetInfo, callback ...func(res types.IResponse)) (types.IResponse, error) {
	payload := GetPayLoad(traceid, own.Service.Name, "", "", "", router)
	if info != nil {
		if info.TargetAddress == "" || info.TargetPort == 0 {
			return nil, errors.New("目标地址或端口错误")
		}
		payload.TargetAddress = info.TargetAddress
		payload.TargetPort = info.TargetPort
		if info.TargetService != "" {
			payload.TargetService = info.TargetService
		}
		if info.TargetPath != "" {
			payload.TargetPath = info.TargetPath
		}
		if info.TargetSocketPort == 0 {
			payload.TargetSocketPort = own.Config.SocketPort
		} else {
			payload.TargetSocketPort = info.TargetSocketPort
		}
		if info.TargetToken != "" {
			payload.Token = info.TargetToken
		}
	}
	return own.CallService(payload, callback...)
}
func (own *ServiceContext) CallServiceUseApi(api types.IRouter) (types.IResponse, error) {
	info := api.RouterInfo()
	pl := &types.PayLoad{
		TraceID:       strconv.Itoa(int(own.NewID())),
		SourceService: own.Service.Name,
		SourcePath:    "",
		TargetService: info.ServiceName,
		TargetPath:    info.Path,
		UserId:        "",
		UserName:      "",
		ClientIP:      utils.GetLocalIP(),
		Auth:          false,
		Instance:      api,
		HttpMethod:    info.Method,
	}
	return own.CallService(pl)
}
func (own *ServiceContext) CallService(payload *types.PayLoad, callback ...func(res types.IResponse)) (types.IResponse, error) {
	res := &Response{}
	if callback != nil {
		ch := make(chan types.IResponse)
		go func(own *ServiceContext, errcallback ...func(res types.IResponse)) {
			values, err := own.sendPayload(context.Background(), payload)
			if err != nil {
				for _, ecb := range errcallback {
					res.err = err
					ecb(res)
				}
				close(ch)
			}
			json.Unmarshal(values, res)
			ch <- res
		}(own, callback[1:]...)
		res := <-ch
		if res != nil {
			callback[0](res)
		}
	} else {
		values, err := own.sendPayload(context.Background(), payload)
		if err != nil {
			logx.Errorw("service_call_failed",
				logx.Field("service", own.Service.Name),
				logx.Field("target_service", payload.TargetService),
				logx.Field("target_route", payload.TargetPath),
				logx.Field("trace_id", payload.TraceID),
				logx.Field("error", err),
			)
			return nil, err
		}
		err = json.Unmarshal(values, res)
		if err != nil {
			logx.Errorw("service_response_decode_failed",
				logx.Field("service", own.Service.Name),
				logx.Field("target_service", payload.TargetService),
				logx.Field("target_route", payload.TargetPath),
				logx.Field("trace_id", payload.TraceID),
				logx.Field("response_bytes", len(values)),
				logx.Field("error", err),
			)
			return nil, err
		}
	}
	return res, nil
}

// sendPayload dispatches a payload. When a TransportSelector is configured,
// the transport chain is retried with exponential backoff; on exhaustion the
// legacy HTTP path is tried once as a final fallback. Without a
// TransportSelector, the legacy path is called exactly once (no retry) to
// avoid duplicating non-idempotent operations.
func (own *ServiceContext) sendPayload(ctx context.Context, payload *types.PayLoad) ([]byte, error) {
	if own.TransportSelector != nil {
		target := payload.TargetAddress
		if payload.TargetPort > 0 {
			target = target + ":" + strconv.Itoa(payload.TargetPort)
		}
		maxRetries := own.Config.Transport.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 1
		}
		retryDelay := own.Config.Transport.RetryDelay

		var lastErr error
		for attempt := 0; attempt < maxRetries; attempt++ {
			result, err := transport.SendWithFallback(ctx, own.TransportSelector, payload, target)
			if err == nil {
				return result, nil
			}
			lastErr = err
			logx.Debugw("transport_retry",
				logx.Field("service", own.Service.Name),
				logx.Field("target_service", payload.TargetService),
				logx.Field("attempt", attempt+1),
				logx.Field("max_attempts", maxRetries),
				logx.Field("error", err),
			)

			if attempt < maxRetries-1 && retryDelay > 0 {
				sleepDuration := retryDelay * time.Duration(1<<attempt)
				if sleepDuration > 5*time.Second {
					sleepDuration = 5 * time.Second
				}
				timer := time.NewTimer(sleepDuration)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
		}
		// All transport retries exhausted; one-shot legacy HTTP fallback.
		logx.Infow("transport_fallback",
			logx.Field("service", own.Service.Name),
			logx.Field("target_service", payload.TargetService),
			logx.Field("attempts", maxRetries),
			logx.Field("fallback_transport", "legacy_http"),
			logx.Field("error", lastErr),
		)
		return own.Service.CallService(payload)
	}
	// No TransportSelector: one-shot legacy path, no retry.
	return own.Service.CallService(payload)
}

// makeCrossNodeSender creates a cross-node sender that routes through
// the configured TransportSelector when available.
func (own *ServiceContext) makeCrossNodeSender() cluster.CrossNodeSender {
	return func(ctx context.Context, target string, data []byte, path string) ([]byte, error) {
		host, portStr, err := net.SplitHostPort(target)
		if err != nil {
			host = target
			portStr = "80"
		}
		port, _ := strconv.Atoi(portStr)
		payload := &types.PayLoad{
			TargetAddress: host,
			TargetPort:    port,
			TargetPath:    path,
			Data:          data,
			HttpMethod:    "POST",
			Auth:          true,
			SourceService: own.Service.Name,
		}
		return transport.SendWithFallback(ctx, own.TransportSelector, payload, target)
	}
}

func initCluster(sc *ServiceContext) error {
	provider, err := cluster.BuildProvider(&sc.Config.Cluster, processLocalRegistry)
	if err != nil {
		return err
	}
	sc.ClusterProvider = provider
	if provider != nil {
		sc.ClusterSwitcher = cluster.NewClusterSwitcher(provider, sc.Service.Name)
	} else {
		sc.ClusterSwitcher = nil
	}
	return nil
}

// claimMachineID registers this service in the process-local cluster registry.
// If the configured MachineID is already taken, it auto-allocates the next free ID.
// Returns the (possibly new) MachineID to use for Snowflake initialisation.
func claimMachineID(con *config.ServerConfig, serviceName string) (int64, error) {
	ctx := context.Background()
	dc := int64(con.DataCenterID)
	machine := int64(con.MachineID)

	nodeID := fmt.Sprintf("%s-%d-%d", serviceName, dc, machine)
	node := &cluster.NodeInfo{
		ID:           nodeID,
		ServiceName:  serviceName,
		DataCenterID: dc,
		MachineID:    machine,
		Address:      "127.0.0.1",
		Port:         con.Port,
		Weight:       1,
	}

	err := processLocalRegistry.Register(ctx, node)
	if err == nil {
		return machine, nil
	}

	// Slot conflict — auto-allocate.
	maxMachineID := int64(1023)
	if con.Cluster.Claim.MachineIDMax > 0 {
		maxMachineID = int64(con.Cluster.Claim.MachineIDMax)
	}
	newMachine := processLocalRegistry.AllocateMachineID(serviceName, dc, maxMachineID)
	if newMachine < 0 {
		return machine, fmt.Errorf("cluster: all MachineID slots are full for DataCenterID=%d", dc)
	}
	node.ID = fmt.Sprintf("%s-%d-%d", serviceName, dc, newMachine)
	node.MachineID = newMachine
	if regErr := processLocalRegistry.Register(ctx, node); regErr != nil {
		return machine, regErr
	}
	logx.Infof("cluster: auto-allocated MachineID=%d for %s (was %d)", newMachine, serviceName, machine)
	return newMachine, nil
}

func GetResponseData[T any](response interface{}) *T {
	res := &Response{}
	bytes, err := json.Marshal(response)
	if err != nil {
		logx.Error(err)
		return nil
	}
	err = json.Unmarshal(bytes, res)
	if err != nil {
		logx.Error(err)
		return nil
	}
	data := new(T)
	res.GetData(data)
	return data
}
func GetInstance[T any](instance interface{}) *T {
	bytes, err := json.Marshal(instance)
	if err != nil {
		logx.Error(err)
		return nil
	}
	data := new(T)
	err = json.Unmarshal(bytes, data)
	if err != nil {
		logx.Error(err)
		return nil
	}
	return data
}
