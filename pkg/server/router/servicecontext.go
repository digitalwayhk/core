package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ratelimit"
	"github.com/digitalwayhk/core/pkg/server/routecache"
	casdoorauth "github.com/digitalwayhk/core/pkg/server/safe/casdoor"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/yitter/idgenerator-go/idgen"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
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
	Config                   *config.ServerConfig
	Service                  *types.Service
	snow                     idgen.ISnowWorker
	Router                   *ServiceRouter
	isStart                  atomic.Bool
	terminated               bool
	shutdownDone             chan struct{}
	shutdownOnce             sync.Once
	lifecycleMu              sync.Mutex
	lifecycleOpOnce          sync.Once
	lifecycleOp              chan struct{} // 串行化启停和 Provider 切换，但不在 Provider 调用期间持有状态锁。
	Pid                      int
	Hub                      interface{} `json:"-"`
	StateChan                chan bool   `json:"-"`
	serverOption             *types.ServerOption
	serverOptionMu           sync.RWMutex
	TransportSelector        transport.TransportSelector     `json:"-"`
	TransportStats           *transport.Stats                `json:"-"`
	MQManager                *mq.MQManager                   `json:"-"`
	EventStream              *event.Stream                   `json:"-"`
	EventBridge              *event.MQBridge                 `json:"-"`
	ServiceEventBridge       *event.ServiceEventBridge       `json:"-"`
	RouteWebSocketHub        *types.RouteWebSocketHub        `json:"-"`
	RouteCacheManager        *routecache.Manager             `json:"-"`
	PublicRateLimiter        *ratelimit.Manager              `json:"-"`
	AuthHookProvider         types.IAuthHookProvider         `json:"-"`
	AuthRequestHookProvider  types.IAuthRequestHookProvider  `json:"-"`
	CasdoorEventHookProvider types.ICasdoorEventHookProvider `json:"-"`
	CasdoorClients           *casdoorauth.ClientSet          `json:"-"`
	AuthRevocationManager    *authstate.Manager              `json:"-"`
	ClusterProvider          cluster.DiscoveryProvider       `json:"-"`
	localFallbackProvider    cluster.DiscoveryProvider       `json:"-"`
	ClusterSwitcher          cluster.ProviderSwitcher        `json:"-"`
	ServiceResolver          *ServiceResolver                `json:"-"`
	ownsClusterProvider      bool
	membership               *cluster.MembershipManager     `json:"-"`
	CrossNodeBroker          *cluster.CrossNodeNoticeBroker `json:"-"`
	nodeID                   string
	configFingerprint        string
	grpcServer               types.GRPCServerLifecycle
	grpcSupervisorOnce       sync.Once
	runtimeErrMu             sync.RWMutex
	runtimeErr               error
	runtimeFailure           chan error
	lifecycleTimeout         time.Duration
	shutdownErrMu            sync.RWMutex
	shutdownErr              error
}

const grpcLifecycleTimeout = 5 * time.Second

// SetLifecycleTimeout 配置服务注册和停止的有界等待时间。
func (own *ServiceContext) SetLifecycleTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	own.lifecycleMu.Lock()
	own.lifecycleTimeout = timeout
	own.lifecycleMu.Unlock()
}

func (own *ServiceContext) lifecycleDuration() time.Duration {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	if own.lifecycleTimeout > 0 {
		return own.lifecycleTimeout
	}
	return grpcLifecycleTimeout
}

// ShutdownError 返回关闭期间首个未恢复错误；重复停止保持同一结果。
func (own *ServiceContext) ShutdownError() error {
	own.shutdownErrMu.RLock()
	defer own.shutdownErrMu.RUnlock()
	return own.shutdownErr
}

func (own *ServiceContext) recordShutdownError(err error) {
	if err == nil {
		return
	}
	own.shutdownErrMu.Lock()
	if own.shutdownErr == nil {
		own.shutdownErr = err
	}
	own.shutdownErrMu.Unlock()
}

type managedGRPCService struct {
	owner  *ServiceContext
	server types.GRPCServerLifecycle
}

func (s *managedGRPCService) Start() { s.server.Start() }
func (s *managedGRPCService) Stop()  { s.owner.SetRunState(false) }

// RuntimeError 返回服务运行期的稳定终态错误。
func (own *ServiceContext) RuntimeError() error {
	own.runtimeErrMu.RLock()
	defer own.runtimeErrMu.RUnlock()
	return own.runtimeErr
}

func (own *ServiceContext) setRuntimeError(err error) {
	if err == nil {
		return
	}
	own.runtimeErrMu.Lock()
	if own.runtimeErr == nil {
		own.runtimeErr = err
		if own.runtimeFailure == nil {
			own.runtimeFailure = make(chan error, 1)
		}
		own.runtimeFailure <- err
	}
	own.runtimeErrMu.Unlock()
}

// Failure 返回服务运行期终态错误。每个 ServiceContext 最多发布一次。
func (own *ServiceContext) Failure() <-chan error {
	own.runtimeErrMu.Lock()
	defer own.runtimeErrMu.Unlock()
	if own.runtimeFailure == nil {
		own.runtimeFailure = make(chan error, 1)
		if own.runtimeErr != nil {
			own.runtimeFailure <- own.runtimeErr
		}
	}
	return own.runtimeFailure
}

// SetGRPCServer 将服务专属 gRPC 生命周期交给 ServiceContext 管理。
func (own *ServiceContext) SetGRPCServer(server types.GRPCServerLifecycle) {
	own.lifecycleMu.Lock()
	own.grpcServer = server
	own.lifecycleMu.Unlock()
	own.superviseGRPC(server)
}

func (own *ServiceContext) waitForGRPCReady(ctx context.Context, server types.GRPCServerLifecycle) error {
	if server == nil {
		return nil
	}
	select {
	case <-server.Ready():
		select {
		case <-server.Done():
			if err := server.Err(); err != nil {
				return err
			}
			return errors.New("grpc server stopped before discovery registration")
		default:
			return nil
		}
	case <-server.Done():
		if err := server.Err(); err != nil {
			return err
		}
		return errors.New("grpc server stopped before becoming ready")
	case <-ctx.Done():
		return fmt.Errorf("wait for grpc ready: %w", ctx.Err())
	}
}

func (own *ServiceContext) superviseGRPC(server types.GRPCServerLifecycle) {
	if server == nil {
		return
	}
	own.grpcSupervisorOnce.Do(func() {
		go func() {
			<-server.Done()
			if err := server.Err(); err != nil {
				own.setRuntimeError(err)
			}
			if own.RuntimeError() != nil {
				own.SetRunState(false)
			}
		}()
	})
}

func (own *ServiceContext) beginLifecycleOperation() {
	_ = own.beginLifecycleOperationContext(context.Background())
}

func (own *ServiceContext) beginLifecycleOperationContext(ctx context.Context) error {
	own.lifecycleOpOnce.Do(func() {
		own.lifecycleOp = make(chan struct{}, 1)
		own.lifecycleOp <- struct{}{}
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-own.lifecycleOp:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

// GetAuthRequestRuntime 返回一次请求使用的认证运行时快照。
// active 为 false 表示 ServiceContext 已进入终止阶段，新认证必须 fail closed。
func (own *ServiceContext) GetAuthRequestRuntime() (*authstate.Manager, types.IAuthRequestHookProvider, bool) {
	if own == nil {
		return nil, nil, false
	}
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	if own.terminated {
		return nil, nil, false
	}
	return own.AuthRevocationManager, own.AuthRequestHookProvider, true
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
		subscriberID := ""
		if own.Service != nil {
			subscriberID = own.Service.Name
		}
		own.ServiceEventBridge = event.NewServiceEventBridge(own.EventStream, event.ServiceEventBridgeOptions{
			SubscriberID: subscriberID,
		})
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
			con.AttachServices = make(map[string]*config.AttachAddress)
			for _, as := range sc.Service.AttachService {
				con.SetAttachService(as.ServiceName, "", 0)
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
	initialized := false
	defer func() {
		if initialized {
			return
		}
		failure := recover()
		cleanupInitializedServiceContext(sc)
		if failure != nil {
			panic(failure)
		}
	}()
	if provider, ok := service.(types.IAuthHookProvider); ok {
		sc.AuthHookProvider = provider
	}
	if provider, ok := service.(types.IAuthRequestHookProvider); ok {
		sc.AuthRequestHookProvider = provider
	}
	if provider, ok := service.(types.ICasdoorEventHookProvider); ok {
		sc.CasdoorEventHookProvider = provider
	}
	assertServiceRoutesRegistrationMutable(sc.Service.Name, sc.Service.Routers)
	sc.localFallbackProvider = processLocalRegistry
	sc.EventStream = event.NewStream()
	sc.ServiceEventBridge = event.NewServiceEventBridge(sc.EventStream, event.ServiceEventBridgeOptions{
		SubscriberID: sc.Service.Name,
	})
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
	protocols := append([]string{con.Transport.Internal}, con.Transport.Fallback...)
	sc.ServiceResolver = NewServiceResolver(sc.ClusterProvider, GetContext, protocols...)
	if sel, selErr := transport.BuildSelector(con.Transport); selErr != nil {
		// Any error from BuildSelector means the user explicitly configured a
		// transport protocol that cannot be built (e.g. quic, mq not yet implemented).
		// This is a hard misconfiguration — prevent silent fallback to legacy HTTP.
		panic(fmt.Sprintf("transport: init failed: %v", selErr))
	} else if sel != nil {
		sc.TransportStats = &transport.Stats{}
		if defaultSelector, ok := sel.(*transport.DefaultSelector); ok {
			defaultSelector.SetStats(sc.TransportStats)
		}
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

	casdoorEnabled := con.Auth.CasDoor.Enable || con.ManageAuth.CasDoor.Enable
	if casdoorEnabled {
		clients, err := casdoorauth.NewClientSet(con.Auth.CasDoor, con.ManageAuth.CasDoor)
		if err != nil {
			panic(fmt.Sprintf("auth lifecycle: Casdoor client init failed: %v", err))
		}
		sc.CasdoorClients = clients
		if con.AuthRevocation.Mode == config.AuthRevocationModeShared && sc.EventBridge == nil {
			panic("auth lifecycle: shared mode requires MQ event-stream")
		}
		manager, err := authstate.NewManager(
			sc.Service.Name,
			con.AuthRevocation,
			authstate.WithEventBridge(sc.ServiceEventBridge),
			authstate.WithCasdoorEventHook(sc.CasdoorEventHookProvider),
		)
		if err != nil {
			panic(fmt.Sprintf("auth lifecycle: revocation manager init failed: %v", err))
		}
		sc.AuthRevocationManager = manager
	}

	// shared 缓存必须等 MQ/EventBridge 外部适配器装配完成后再初始化，确保
	// Redis 事实缓存与跨节点失效订阅同时就绪，不允许只启动本地层。
	cacheManager, cacheErr := routecache.NewManager(
		sc.Service.Name,
		con.RouteCache,
		routecache.WithInvalidationBridge(sc.ServiceEventBridge),
	)
	if cacheErr != nil {
		panic(fmt.Sprintf("route cache: init failed: %v", cacheErr))
	}
	sc.RouteCacheManager = cacheManager
	sc.PublicRateLimiter = ratelimit.NewManager(sc.Service.Name, 0)

	sc.snow = utils.NewAlgorithmSnowFlake(con.MachineID, con.DataCenterID)
	sc.Router = NewServiceRouter(sc, service)
	initialized = true
}

func cleanupInitializedServiceContext(sc *ServiceContext) {
	if sc == nil {
		return
	}
	if sc.AuthRevocationManager != nil {
		sc.AuthRevocationManager.BeginClose()
	}
	if sc.RouteWebSocketHub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = sc.RouteWebSocketHub.Close(ctx)
		cancel()
	}
	if sc.RouteCacheManager != nil {
		sc.RouteCacheManager.Close()
	}
	if sc.PublicRateLimiter != nil {
		sc.PublicRateLimiter.Close()
	}
	if sc.ServiceEventBridge != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = sc.ServiceEventBridge.Close(ctx)
		cancel()
	}
	if sc.MQManager != nil {
		_ = sc.MQManager.Close()
	}
	if sc.AuthRevocationManager != nil {
		_ = sc.AuthRevocationManager.Close()
	}
	if sc.ServiceResolver != nil {
		sc.ServiceResolver.Close()
	}
	if sc.ownsClusterProvider && sc.ClusterProvider != nil {
		_ = sc.ClusterProvider.Close()
	}
	sc.AuthRevocationManager = nil
	sc.CasdoorClients = nil
	sc.RouteWebSocketHub = nil
	sc.RouteCacheManager = nil
	sc.PublicRateLimiter = nil
	sc.EventBridge = nil
	sc.ServiceEventBridge = nil
	sc.MQManager = nil
	sc.EventStream = nil
	sc.ServiceResolver = nil
	sc.ClusterProvider = nil
	sc.ownsClusterProvider = false
}

func assertServiceRoutesRegistrationMutable(owner string, routes []types.IRouter) {
	for _, route := range routes {
		if route == nil {
			continue
		}
		route.RouterInfo().PrepareRegistration(owner)
	}
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

// UseOutbox 启用当前服务的可靠 Outbox 发布器。事件来源服务名固定为当前 ServiceContext。
func (own *ServiceContext) UseOutbox(store event.OutboxStore) error {
	if own == nil || own.ServiceEventBridge == nil || own.Service == nil {
		return event.ErrServiceEventBridgeClosed
	}
	return own.ServiceEventBridge.UseOutbox(event.OutboxOptions{
		SourceService: own.Service.Name,
		Store:         store,
		External:      own.ServiceEventBridge.HasExternalPublisher(),
	})
}

// NotifyOutbox 唤醒当前服务 Outbox 发布器尽快扫描本地 Outbox 表。
func (own *ServiceContext) NotifyOutbox() {
	if own == nil || own.ServiceEventBridge == nil {
		return
	}
	own.ServiceEventBridge.NotifyOutbox()
}

// SubscribeEvent 注册统一业务事件订阅；运行时负责连接本地事件中心和外部事件桥。
func (own *ServiceContext) SubscribeEvent(subscription event.Subscription) (func(), error) {
	if own == nil || own.ServiceEventBridge == nil {
		return nil, event.ErrServiceEventBridgeClosed
	}
	return own.ServiceEventBridge.SubscribeEvent(subscription)
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
		if state {
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
	switcher := own.ClusterSwitcher
	membership := own.membership
	broker := own.CrossNodeBroker
	mqManager := own.MQManager
	serviceEventBridge := own.ServiceEventBridge
	routeWebSocketHub := own.RouteWebSocketHub
	routeCacheManager := own.RouteCacheManager
	publicRateLimiter := own.PublicRateLimiter
	authRevocationManager := own.AuthRevocationManager
	serviceResolver := own.ServiceResolver
	ownsClusterProvider := own.ownsClusterProvider
	grpcServer := own.grpcServer
	if !state {
		own.membership = nil
		own.CrossNodeBroker = nil
		own.MQManager = nil
		own.EventBridge = nil
		own.ServiceEventBridge = nil
		own.RouteWebSocketHub = nil
		own.RouteCacheManager = nil
		own.PublicRateLimiter = nil
		own.AuthRevocationManager = nil
		own.CasdoorClients = nil
		own.AuthRequestHookProvider = nil
		own.CasdoorEventHookProvider = nil
		own.AuthHookProvider = nil
		own.EventStream = nil
		own.ServiceResolver = nil
		own.ownsClusterProvider = false
	}
	own.lifecycleMu.Unlock()
	if !state && authRevocationManager != nil {
		authRevocationManager.BeginClose()
	}
	if !state && own.Router != nil {
		own.Router.unregisterRouterInfos()
	}
	if !state {
		defer func() {
			contextRegistry.remove(own.Service.Name, own)
			own.completeShutdown()
		}()
	}

	if state {
		readyCtx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
		readyErr := own.waitForGRPCReady(readyCtx, grpcServer)
		cancel()
		if readyErr != nil {
			own.setRuntimeError(readyErr)
			own.isStart.Store(false)
			if own.Config.Cluster.Mode == "on" {
				panic(fmt.Sprintf("grpc: service %s failed before discovery registration: %v", own.Service.Name, readyErr))
			}
			return
		}
		nodeID, node, interval := own.clusterMembershipConfig()
		if provider != nil && membership == nil {
			var err error
			membership, err = own.startMembership(provider, node, interval, grpcServer)
			if err != nil {
				if own.Config.Cluster.Mode == "auto" && provider != own.localFallback() {
					failedProvider := provider
					registerErr := err
					if cleanupErr := own.cleanupFailedRegistration(failedProvider, node.ID, registerErr); cleanupErr != nil {
						err = errors.Join(registerErr, cleanupErr)
					} else {
						provider = own.localFallback()
						logx.Infow("cluster_degraded",
							logx.Field("service", own.Service.Name),
							logx.Field("provider", failedProvider.Name()),
							logx.Field("fallback_provider", provider.Name()),
							logx.Field("error", registerErr),
						)
						membership, err = own.startMembership(provider, node, interval, grpcServer)
					}
					if err == nil {
						own.lifecycleMu.Lock()
						own.ClusterProvider = provider
						own.ownsClusterProvider = false
						own.ClusterSwitcher = cluster.NewClusterSwitcher(provider, own.Service.Name)
						if own.ServiceResolver != nil {
							own.ServiceResolver.SetProvider(provider)
						}
						own.lifecycleMu.Unlock()
						if ownsClusterProvider {
							if closeErr := failedProvider.Close(); closeErr != nil {
								logx.Errorw("cluster_provider_close_failed",
									logx.Field("service", own.Service.Name),
									logx.Field("provider", failedProvider.Name()),
									logx.Field("error", closeErr),
								)
							}
						}
					}
				}
				if err != nil {
					startupErr := fmt.Errorf("cluster registration failed for service %s using provider %s: %w",
						own.Service.Name, provider.Name(), err)
					own.failStartup(startupErr, grpcServer)
					if own.Config.Cluster.Mode == "on" {
						panic(startupErr)
					}
					return
				}
			}
		}
		if provider != nil && membership != nil && broker == nil {
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
		own.superviseGRPC(grpcServer)
	} else {
		if shutdown, ok := switcher.(cluster.ProviderSwitchShutdown); ok {
			ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
			own.recordShutdownError(shutdown.Shutdown(ctx, provider))
			cancel()
		}
		if grpcServer != nil {
			grpcServer.BeginShutdown()
		}
		if membership != nil {
			ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
			own.recordShutdownError(membership.Stop(ctx))
			cancel()
			membership = nil
		}
		if grpcServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
			if err := grpcServer.StopContext(ctx); err != nil {
				own.recordShutdownError(err)
				logx.Errorw("grpc_server_stop_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("error", err),
				)
			}
			cancel()
		}
		if stopper, ok := own.TransportSelector.(interface{ Stop(context.Context) error }); ok {
			ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
			if err := stopper.Stop(ctx); err != nil {
				own.recordShutdownError(err)
				logx.Errorw("transport_client_pool_stop_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("error", err),
				)
			}
			cancel()
		}
		if serviceResolver != nil {
			serviceResolver.Close()
		}
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
		if publicRateLimiter != nil {
			publicRateLimiter.Close()
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
		if ownsClusterProvider && provider != nil {
			if err := provider.Close(); err != nil {
				logx.Errorw("cluster_provider_close_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("provider", provider.Name()),
					logx.Field("error", err),
				)
			}
		}
		if mqManager != nil {
			if err := mqManager.Close(); err != nil {
				logx.Errorf("mq: close failed: %v", err)
			}
		}
		if authRevocationManager != nil {
			if err := authRevocationManager.Close(); err != nil {
				logx.Errorw("auth_revocation_manager_close_failed",
					logx.Field("service", own.Service.Name),
					logx.Field("error", err),
				)
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
	return own.syncProviderAfterSwitch()
}

// ClusterProviderSnapshot reads provider identity and nodes within the service
// lifecycle boundary so switching and shutdown cannot replace or close it.
func (own *ServiceContext) ClusterProviderSnapshot(
	ctx context.Context,
	serviceName string,
	statuses ...cluster.NodeStatus,
) (string, []*cluster.NodeInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := own.beginLifecycleOperationContext(ctx); err != nil {
		return "", nil, fmt.Errorf("cluster: wait for provider query: %w", err)
	}
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	provider := own.ClusterProvider
	terminated := own.terminated
	own.lifecycleMu.Unlock()
	if terminated || provider == nil {
		return "none", []*cluster.NodeInfo{}, nil
	}
	providerName := provider.Name()
	nodes, err := provider.List(ctx, serviceName, statuses...)
	if err != nil {
		return "", nil, err
	}
	return providerName, nodes, nil
}

func (own *ServiceContext) syncProviderAfterSwitch() error {

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
	running := own.isStart.Load()
	membership := own.membership
	broker := own.CrossNodeBroker
	nodeID := own.nodeID
	own.lifecycleMu.Unlock()
	if !running {
		own.lifecycleMu.Lock()
		own.ClusterProvider = newProvider
		own.ownsClusterProvider = newProvider != nil && newProvider != processLocalRegistry
		if own.ServiceResolver != nil {
			own.ServiceResolver.SetProvider(newProvider)
		}
		own.lifecycleMu.Unlock()
		return nil
	}

	if membership != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
		stopErr := membership.Stop(stopCtx)
		cancel()
		if stopErr != nil {
			err := fmt.Errorf("switch discovery provider for service %s node %s from %s to %s: stop old membership: %w",
				own.Service.Name, nodeID, providerName(oldProvider), providerName(newProvider), stopErr)
			own.setRuntimeError(err)
			own.isStart.Store(false)
			return err
		}
	}

	nodeID, node, interval := own.clusterMembershipConfig()
	var newMembership *cluster.MembershipManager
	var newBroker *cluster.CrossNodeNoticeBroker
	if newProvider != nil {
		var err error
		newMembership, err = own.startMembership(newProvider, node, interval, own.grpcServer)
		if err != nil {
			switchErr := fmt.Errorf("switch discovery provider for service %s node %s from %s to %s: register new membership: %w",
				own.Service.Name, nodeID, providerName(oldProvider), providerName(newProvider), err)
			own.setRuntimeError(switchErr)
			own.isStart.Store(false)
			return switchErr
		}
		newBroker = cluster.NewCrossNodeNoticeBroker(newProvider, own.Service.Name, nodeID)
		if own.TransportSelector != nil {
			newBroker.SetSender(own.makeCrossNodeSender())
		}
	}
	if broker != nil {
		types.ClearCrossNodeForwarderForService(own.Service.Name, broker)
		drainCtx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
		broker.DrainAndStop(drainCtx)
		cancel()
	}
	if newBroker != nil {
		types.SetCrossNodeForwarderForService(own.Service.Name, newBroker)
	}

	own.lifecycleMu.Lock()
	own.ClusterProvider = newProvider
	own.ownsClusterProvider = newProvider != nil && newProvider != processLocalRegistry
	if own.ServiceResolver != nil {
		own.ServiceResolver.SetProvider(newProvider)
	}
	own.nodeID = nodeID
	own.membership = newMembership
	own.CrossNodeBroker = newBroker
	own.lifecycleMu.Unlock()
	return nil
}

// BeginProviderSwitch starts a provider migration inside the service lifecycle
// boundary. A target not accepted by Begin is closed before this method returns.
func (own *ServiceContext) BeginProviderSwitch(ctx context.Context, to cluster.DiscoveryProvider) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := own.beginLifecycleOperationContext(ctx); err != nil {
		return closeRejectedSwitchTarget(ctx, to, err)
	}
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	terminated := own.terminated
	switcher := own.ClusterSwitcher
	own.lifecycleMu.Unlock()
	if terminated {
		return closeRejectedSwitchTarget(ctx, to, errors.New("cluster: service context is terminated"))
	}
	if switcher == nil {
		return closeRejectedSwitchTarget(ctx, to, errors.New("cluster: switcher not initialised"))
	}
	return beginProviderSwitch(ctx, switcher, to)
}

// CompleteProviderSwitch promotes, synchronizes, and finalizes a provider
// migration as one lifecycle operation, so shutdown cannot interleave.
func (own *ServiceContext) CompleteProviderSwitch(ctx context.Context) error {
	own.beginLifecycleOperation()
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	terminated := own.terminated
	running := own.isStart.Load()
	switcher := own.ClusterSwitcher
	own.lifecycleMu.Unlock()
	if terminated {
		return errors.New("cluster: service context is terminated")
	}
	if switcher == nil {
		return errors.New("cluster: switcher not initialised")
	}
	if transaction, ok := switcher.(cluster.ProviderSwitchTransaction); ok {
		if err := transaction.Promote(ctx); err != nil {
			return err
		}
		if err := own.syncProviderAfterSwitch(); err != nil {
			return err
		}
		return transaction.Finalize(ctx)
	}
	if running {
		return errors.New("cluster: running provider switch requires transactional switcher")
	}
	if err := switcher.Complete(ctx); err != nil {
		return err
	}
	return own.syncProviderAfterSwitch()
}

// RollbackProviderSwitch serializes a management rollback with service
// startup and shutdown.
func (own *ServiceContext) RollbackProviderSwitch(ctx context.Context) error {
	own.beginLifecycleOperation()
	defer own.endLifecycleOperation()

	own.lifecycleMu.Lock()
	terminated := own.terminated
	switcher := own.ClusterSwitcher
	own.lifecycleMu.Unlock()
	if terminated {
		return errors.New("cluster: service context is terminated")
	}
	if switcher == nil {
		return errors.New("cluster: switcher not initialised")
	}
	return switcher.Rollback(ctx)
}

func beginProviderSwitch(
	ctx context.Context,
	switcher cluster.ProviderSwitcher,
	to cluster.DiscoveryProvider,
) error {
	err := switcher.Begin(ctx, to)
	if err == nil {
		return nil
	}
	return closeRejectedSwitchTarget(ctx, to, err)
}

func closeRejectedSwitchTarget(
	ctx context.Context,
	to cluster.DiscoveryProvider,
	beginErr error,
) error {
	if to == nil {
		return beginErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	closeDone := make(chan error, 1)
	go func() {
		if closer, ok := to.(cluster.ContextCloser); ok {
			closeDone <- closer.CloseContext(ctx)
			return
		}
		closeDone <- to.Close()
	}()
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			return errors.Join(beginErr, fmt.Errorf("cluster: close rejected target provider %s: %w", to.Name(), closeErr))
		}
		return beginErr
	case <-ctx.Done():
		return errors.Join(beginErr, fmt.Errorf("cluster: wait for rejected target provider %s close: %w", to.Name(), ctx.Err()))
	}
}

func providerName(provider cluster.DiscoveryProvider) string {
	if provider == nil {
		return "off"
	}
	return provider.Name()
}

func (own *ServiceContext) clusterMembershipConfig() (string, *cluster.NodeInfo, time.Duration) {
	nodeID := fmt.Sprintf("%s-%d-%d", own.Service.Name,
		own.Config.DataCenterID, own.Config.MachineID)
	address := own.Config.RunIp
	if own.Config.Cluster.AdvertiseAddress != "" {
		address = own.Config.Cluster.AdvertiseAddress
	}
	node := &cluster.NodeInfo{
		ID:           nodeID,
		ServiceName:  own.Service.Name,
		DataCenterID: int64(own.Config.DataCenterID),
		MachineID:    int64(own.Config.MachineID),
		Address:      address,
		Port:         own.Config.Port,
		GRPCPort:     own.Config.Transport.GRPC.Port,
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
	grpcServer types.GRPCServerLifecycle,
) (*cluster.MembershipManager, error) {
	registerCtx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
	defer cancel()
	registerDone := make(chan struct{})
	if grpcServer != nil {
		go func() {
			select {
			case <-grpcServer.Done():
				cancel()
			case <-registerDone:
			}
		}()
	}
	if err := provider.Register(registerCtx, node); err != nil {
		close(registerDone)
		return nil, fmt.Errorf("register node %s: %w", node.ID, err)
	}
	close(registerDone)
	if grpcServer != nil {
		select {
		case <-grpcServer.Done():
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
			cleanupErr := provider.Deregister(cleanupCtx, node.ID)
			cleanupCancel()
			serveErr := grpcServer.Err()
			if serveErr == nil {
				serveErr = errors.New("grpc server stopped during discovery registration")
			}
			if cleanupErr != nil && !errors.Is(cleanupErr, cluster.ErrNodeNotFound) {
				return nil, errors.Join(serveErr, fmt.Errorf("revoke node %s: %w", node.ID, cleanupErr))
			}
			return nil, serveErr
		default:
		}
	}
	membership := cluster.NewMembershipManager(provider, node.ID, interval)
	membership.Start(context.Background())
	return membership, nil
}

func (own *ServiceContext) cleanupFailedRegistration(
	provider cluster.DiscoveryProvider,
	nodeID string,
	registerErr error,
) error {
	var registrationErr *cluster.RegistrationError
	if errors.As(registerErr, &registrationErr) && registrationErr.Compensated {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
	defer cancel()
	err := provider.Deregister(cleanupCtx, nodeID)
	if registrationErr != nil {
		if err == nil || errors.Is(err, cluster.ErrNodeNotFound) {
			return nil
		}
		return errors.Join(
			fmt.Errorf("cluster registration compensation was not confirmed: %w", registerErr),
			fmt.Errorf("cleanup failed cluster registration for node %s using provider %s: %w",
				nodeID, provider.Name(), err),
		)
	}
	if err == nil || errors.Is(err, cluster.ErrNodeNotFound) {
		return nil
	}
	return fmt.Errorf("cleanup failed cluster registration for node %s using provider %s: %w",
		nodeID, provider.Name(), err)
}

func (own *ServiceContext) localFallback() cluster.DiscoveryProvider {
	if own.localFallbackProvider != nil {
		return own.localFallbackProvider
	}
	return processLocalRegistry
}

func (own *ServiceContext) failStartup(err error, grpcServer types.GRPCServerLifecycle) {
	own.setRuntimeError(err)
	own.isStart.Store(false)
	if stopper, ok := own.TransportSelector.(interface{ Stop(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
		own.recordShutdownError(stopper.Stop(ctx))
		cancel()
	}
	if grpcServer == nil {
		return
	}
	grpcServer.BeginShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), own.lifecycleDuration())
	_ = grpcServer.StopContext(ctx)
	cancel()
}

func (own *ServiceContext) IsRun() bool {
	return own.isStart.Load()
}
func (own *ServiceContext) SetHttpServer(server types.IRunServer) {
	own.Service.HttpServer = server
}
func (own *ServiceContext) GetServers() []service.Service {
	items := make([]service.Service, 0, 2+len(own.Service.GetInternalServers()))
	if own.Service.HttpServer != nil {
		items = append(items, own.Service.HttpServer)
	}
	for _, server := range own.Service.GetInternalServers() {
		if server != nil {
			items = append(items, server)
		}
	}
	if own.grpcServer != nil {
		items = append(items, &managedGRPCService{owner: own, server: own.grpcServer})
	}
	return items
}

// HandleInternalPayload 是 gRPC 服务端调用业务路由的入口。
func (own *ServiceContext) HandleInternalPayload(ctx context.Context, payload *types.PayLoad) ([]byte, error) {
	own.TransportStats.RecordInboundGRPC()
	if payload != nil && (own.Service == nil || payload.TargetService != own.Service.Name) {
		return nil, fmt.Errorf("%w: inbound target does not match listener service", ErrTargetServiceUnavailable)
	}
	return own.invokePayload(ctx, payload)
}
func (own *ServiceContext) SetAttachServiceAddress(name string) error {
	if cas, ok := own.Config.AttachServices[name]; ok {
		if as, ok := own.Service.AttachService[name]; ok {
			as.Address = cas.Address
			as.Port = cas.Port
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
		for _, oa := range as.ObserverRouters {
			ti := &types.TargetInfo{
				TargetService: as.ServiceName,
				TargetPath:    info.GetPath(),
			}
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
			values, err := own.invokePayload(context.Background(), v)
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
	if info.TargetService == "" || info.TargetPath == "" {
		return false, errors.New("observeCall TargetService or TargetPath is empty")
	}
	oa.ReceiveService = own.Service.Name
	payload := &types.PayLoad{
		TraceID:       "1",
		SourceService: oa.ReceiveService,
		TargetService: info.TargetService,
		SourcePath:    "",
		TargetPath:    info.TargetPath,
		UserId:        "",
		Auth:          false,
		Instance:      oa,
	}
	values, err := own.invokePayload(context.Background(), payload)
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
		TraceID:       args.TraceID,
		SourceService: args.SendService,
		TargetService: args.ReceiveService,
		SourcePath:    args.Topic,
		TargetPath:    info.GetPath(),
		Auth:          false,
		Instance:      args,
	}
	values, err := ctx.invokePayload(context.Background(), payload)
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
		TargetService: info.GetServiceName(),
		TargetPath:    info.GetPath(),
		UserId:        "",
		UserName:      "",
		ClientIP:      utils.GetLocalIP(),
		Auth:          false,
		Instance:      api,
		HttpMethod:    info.GetMethod(),
	}
	return own.CallService(pl)
}
func (own *ServiceContext) CallService(payload *types.PayLoad, callback ...func(res types.IResponse)) (types.IResponse, error) {
	res := &Response{}
	ctx := context.Background()
	if payload != nil && own != nil && own.Service != nil {
		payload.SourceService = own.Service.Name
		ctx = types.ContextWithTrustedInternalCaller(ctx, own.Service.Name)
	}
	if callback != nil {
		ch := make(chan types.IResponse)
		go func(own *ServiceContext, errcallback ...func(res types.IResponse)) {
			values, err := own.invokePayload(ctx, payload)
			if err != nil {
				for _, ecb := range errcallback {
					res.err = err
					ecb(res)
				}
				ch <- res
				return
			}
			json.Unmarshal(values, res)
			ch <- res
		}(own, callback[1:]...)
		res := <-ch
		if res != nil {
			callback[0](res)
		}
	} else {
		values, err := own.invokePayload(ctx, payload)
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

func (own *ServiceContext) invokePayload(ctx context.Context, payload *types.PayLoad) ([]byte, error) {
	if payload == nil || payload.TargetService == "" || payload.TargetPath == "" {
		return nil, fmt.Errorf("%w: target service and path are required", ErrTargetServiceUnavailable)
	}
	if local := GetContext(payload.TargetService); local != nil {
		return own.dispatchLocal(ctx, payload, local)
	}
	var endpoints transport.TransportEndpoints
	if own.ServiceResolver != nil {
		resolved, err := own.ServiceResolver.Resolve(ctx, payload.TargetService)
		if err != nil {
			return nil, err
		}
		payload.TargetAddress = resolved.Info.TargetAddress
		payload.TargetPort = resolved.Info.TargetPort
		endpoints = resolved.Endpoints
	} else if payload.TargetAddress != "" {
		// 直接指定地址的旧调用只具备 HTTP 端点；gRPC 端点必须来自服务发现。
		endpoints = serviceTransportEndpoints(payload.TargetAddress, payload.TargetPort, 0)
	} else {
		return nil, fmt.Errorf("%w: resolver is unavailable", ErrTargetServiceUnavailable)
	}
	return own.sendPayload(ctx, payload, endpoints)
}

func (own *ServiceContext) dispatchLocal(ctx context.Context, payload *types.PayLoad, target *ServiceContext) ([]byte, error) {
	if target == nil || target.Router == nil {
		return nil, fmt.Errorf("%w: service=%s", ErrTargetServiceUnavailable, payload.TargetService)
	}
	info := target.Router.GetRouter(payload.TargetPath)
	if info == nil {
		return nil, fmt.Errorf("%w: route=%s", ErrTargetServiceUnavailable, payload.TargetPath)
	}
	req := ToRequest(payload)
	if req == nil {
		return nil, fmt.Errorf("%w: request context for %s", ErrTargetServiceUnavailable, payload.TargetService)
	}
	if caller, trusted := types.TrustedInternalCallerFromContext(ctx); trusted {
		req = requestWithTrustedInternalCaller(req, caller)
	}
	if err := info.AuthorizeInternalCaller(req); err != nil {
		return nil, err
	}
	api, err := info.ParseNew(payload.Instance)
	if err != nil {
		return nil, err
	}
	response := info.ExecDo(api, req)
	return json.Marshal(response)
}

// sendPayload 在发送前完成协议选择和健康预检。MaxRetries 只作用于预检；
// 一旦 Transport.Send 开始，无论结果是否确定，都不会重试或切换协议。
func (own *ServiceContext) sendPayload(ctx context.Context, payload *types.PayLoad, endpoints transport.TransportEndpoints) ([]byte, error) {
	if own.TransportSelector != nil {
		maxRetries := own.Config.Transport.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 1
		}
		selection, err := transport.SelectWithRetry(
			ctx, own.TransportSelector, payload, endpoints,
			maxRetries, own.Config.Transport.RetryDelay,
		)
		if err != nil {
			return nil, err
		}
		return transport.SendSelection(ctx, own.TransportSelector, selection, payload)
	}
	// No TransportSelector: one-shot legacy path, no retry.
	return own.Service.CallService(payload)
}

// makeCrossNodeSender creates a cross-node sender that routes through
// the configured TransportSelector when available.
func (own *ServiceContext) makeCrossNodeSender() cluster.CrossNodeSender {
	return func(ctx context.Context, target *cluster.NodeInfo, data []byte, path string) ([]byte, error) {
		payload := &types.PayLoad{
			TargetAddress: target.Address,
			TargetPort:    target.Port,
			TargetPath:    path,
			TargetService: target.ServiceName,
			Data:          data,
			Instance:      json.RawMessage(data),
			HttpMethod:    "POST",
			Auth:          true,
			SourceService: own.Service.Name,
		}
		endpoints := serviceTransportEndpoints(target.Address, target.Port, target.GRPCPort)
		attempts := own.Config.Transport.MaxRetries
		if attempts <= 0 {
			attempts = 1
		}
		selection, err := transport.SelectWithRetry(
			ctx, own.TransportSelector, payload, endpoints,
			attempts, own.Config.Transport.RetryDelay,
		)
		if err != nil {
			return nil, err
		}
		return transport.SendSelection(ctx, own.TransportSelector, selection, payload)
	}
}

func initCluster(sc *ServiceContext) error {
	provider, err := cluster.BuildProvider(&sc.Config.Cluster, processLocalRegistry)
	if err != nil {
		return err
	}
	sc.ClusterProvider = provider
	sc.ownsClusterProvider = provider != nil && provider != processLocalRegistry
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
