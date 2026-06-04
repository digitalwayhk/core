package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/yitter/idgenerator-go/idgen"
	"github.com/zeromicro/go-zero/core/logx"
)

// processLocalRegistry is a shared in-memory cluster registry for all ServiceContexts
// within the same process. This enables intra-process MachineID conflict detection.
var processLocalRegistry = cluster.NewLocalProvider(
	10*time.Second,
	10*time.Second,
	30*time.Second,
)

func init() {
	processLocalRegistry.Start()
}

type ServiceContext struct {
	Config            *config.ServerConfig
	Service           *types.Service
	snow              idgen.ISnowWorker
	Router            *ServiceRouter
	isStart           bool
	Pid               int
	Hub               interface{} `json:"-"`
	StateChan         chan bool   `json:"-"`
	serverOption      *types.ServerOption
	TransportSelector transport.TransportSelector    `json:"-"`
	MQManager         *mq.MQManager                  `json:"-"`
	EventStream       *event.Stream                  `json:"-"`
	EventBridge       *event.MQBridge                `json:"-"`
	ClusterProvider   cluster.DiscoveryProvider      `json:"-"`
	ClusterSwitcher   cluster.ProviderSwitcher       `json:"-"`
	membership        *cluster.MembershipManager     `json:"-"`
	CrossNodeBroker   *cluster.CrossNodeNoticeBroker `json:"-"`
	nodeID            string
}

func (own *ServiceContext) GetServerOption() *types.ServerOption {
	if own != nil && own.serverOption != nil && own.Config != nil {
		own.serverOption.RemoteAccessManageAPI = own.Config.RemoteAccessManageAPI
	}
	return own.serverOption
}
func (own *ServiceContext) SetServerOption(so *types.ServerOption) {
	own.serverOption = so
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
	own.EventBridge = event.NewMQBridge(own.EventStream, own.MQManager)
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

var scontext map[string]*ServiceContext
var TestResult map[string]interface{}

func init() {
	scontext = make(map[string]*ServiceContext)
	TestResult = make(map[string]interface{})
}

func NewServiceContext(service types.IService) *ServiceContext {
	name := strings.ToLower(service.ServiceName())
	if sc, ok := scontext[name]; ok {
		return sc
	}
	sc := &ServiceContext{}
	sc.StateChan = make(chan bool, 1)
	sc.Service = initService(service, sc)
	con := config.ReadConfig(name)
	if con == nil {
		count := len(scontext)
		port := DEFAULTPORT + count
		con = config.NewServiceDefaultConfig(name, port)
		con.DataCenterID = uint(count) + 1
		con.MachineID = 1
		con.SocketPort = DEFAULTSOCKETPORT + count
		con.AttachServices = make(map[string]*config.AttachAddress)
		for _, as := range sc.Service.AttachService {
			con.SetAttachService(as.ServiceName, "", 0, 0)
		}
		err := con.Save()
		if err != nil {
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
	initServiceContextPost(sc, service, con, name)
	return scontext[name]
}

// NewServiceContextWithConfig creates a ServiceContext using the provided
// config directly, bypassing file-based config loading. Intended for testing
// and programmatic service bootstrap where the caller manages configuration.
func NewServiceContextWithConfig(service types.IService, con *config.ServerConfig) *ServiceContext {
	name := strings.ToLower(service.ServiceName())
	if sc, ok := scontext[name]; ok {
		return sc
	}
	sc := &ServiceContext{}
	sc.StateChan = make(chan bool, 1)
	sc.Service = initService(service, sc)
	sc.Config = con
	initServiceContextPost(sc, service, con, name)
	return scontext[name]
}

// initServiceContextPost performs the post-config initialisation shared by
// NewServiceContext and NewServiceContextWithConfig: MachineID claiming,
// cluster/transport/MQ provider setup, Snowflake, and router wiring.
func initServiceContextPost(sc *ServiceContext, service types.IService, con *config.ServerConfig, name string) {
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
		logx.Errorf("cluster: init failed (degraded): %v", err)
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
			if con.MQ.Mode == "on" {
				panic(fmt.Sprintf("mq: required provider init failed (mode=on): %v", mqErr))
			}
			logx.Errorf("mq: init failed (degraded): %v", mqErr)
		} else {
			sc.MQManager = mgr
			// Wire MQ-backed event stream when usage includes "event-stream".
			if mgr != nil && containsUsage(con.MQ.Usage, "event-stream") {
				sc.EnableEventBridge()
			}
		}
	}

	sc.snow = utils.NewAlgorithmSnowFlake(con.MachineID, con.DataCenterID)
	sc.Router = NewServiceRouter(sc, service)
	scontext[name] = sc
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
	TestResult[info.GetPath()] = nil
}
func GetContext(name string) *ServiceContext {
	if name == "" {
		return nil
	}
	return scontext[name]
}
func GetContexts() map[string]*ServiceContext {
	return scontext
}
func (own *ServiceContext) NewID() uint {
	return uint(own.snow.NextId())
}
func (own *ServiceContext) SetPid(pid int) {
	own.Pid = pid
}
func (own *ServiceContext) SetRunState(state bool) {
	own.isStart = state

	if state {
		if own.ClusterProvider != nil && own.membership == nil {
			nodeID := fmt.Sprintf("%s-%d-%d", own.Service.Name,
				own.Config.DataCenterID, own.Config.MachineID)
			own.nodeID = nodeID
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
			if regErr := own.ClusterProvider.Register(context.Background(), node); regErr != nil {
				logx.Errorf("cluster: register node %s: %v", nodeID, regErr)
			} else {
				interval := own.Config.Cluster.HeartbeatInterval
				if interval == 0 {
					interval = 3 * time.Second
				}
				own.membership = cluster.NewMembershipManager(own.ClusterProvider, nodeID, interval)
				own.membership.Start(context.Background())
			}
		}
		if own.ClusterProvider != nil && own.CrossNodeBroker == nil {
			if own.nodeID == "" {
				own.nodeID = fmt.Sprintf("%s-%d-%d", own.Service.Name,
					own.Config.DataCenterID, own.Config.MachineID)
			}
			own.CrossNodeBroker = cluster.NewCrossNodeNoticeBroker(
				own.ClusterProvider, own.Service.Name, own.nodeID,
			)
			types.SetCrossNodeForwarder(own.CrossNodeBroker)
		}
	} else {
		if own.CrossNodeBroker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			own.CrossNodeBroker.DrainAndStop(ctx)
			own.CrossNodeBroker = nil
		}
		if own.membership != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			own.membership.Stop(ctx)
			own.membership = nil
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
	if own.ClusterSwitcher == nil {
		return nil
	}
	newProvider := own.ClusterSwitcher.Current()
	if newProvider == own.ClusterProvider {
		return nil
	}
	own.ClusterProvider = newProvider

	if !own.isStart {
		return nil
	}

	if own.membership != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		own.membership.Stop(stopCtx)
		cancel()
		own.membership = nil
	}
	nodeID := fmt.Sprintf("%s-%d-%d", own.Service.Name,
		own.Config.DataCenterID, own.Config.MachineID)
	own.nodeID = nodeID
	if newProvider != nil {
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
		if regErr := newProvider.Register(context.Background(), node); regErr != nil {
			logx.Errorf("cluster: re-register node %s after switch: %v", nodeID, regErr)
		} else {
			interval := own.Config.Cluster.HeartbeatInterval
			if interval == 0 {
				interval = 3 * time.Second
			}
			own.membership = cluster.NewMembershipManager(newProvider, nodeID, interval)
			own.membership.Start(context.Background())
		}
	}

	if own.CrossNodeBroker != nil {
		drainCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		own.CrossNodeBroker.DrainAndStop(drainCtx)
		cancel()
		own.CrossNodeBroker = nil
	}
	if newProvider != nil {
		own.CrossNodeBroker = cluster.NewCrossNodeNoticeBroker(
			newProvider, own.Service.Name, nodeID,
		)
		types.SetCrossNodeForwarder(own.CrossNodeBroker)
	} else {
		types.SetCrossNodeForwarder(nil)
	}

	return nil
}

func (own *ServiceContext) IsRun() bool {
	return own.isStart
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
				logx.Errorf("%s Observe TargetInfo:%s Error:%s", own.Service.Name, utils.PrintObj(v), err.Error())
			}
			res := &Response{}
			json.Unmarshal(values, res)
			if !res.Success {
				logx.Errorf("%s Observe TargetInfo:%s Error:%s", own.Service.Name, utils.PrintObj(v), res.ErrorMessage)
			} else {
				logx.Infof("%s Observe TargetAddress:%s, TargetService:%s, TargetPath:%s Success", own.Service.Name, v.TargetAddress, v.TargetService, v.TargetPath)
			}
		}
		obseLock.Unlock()
	}
}
func (own *ServiceContext) observeCall(oa *types.ObserveArgs, info *types.TargetInfo) (bool, error) {
	if oa.ServiceName == "" || oa.Topic == "" {
		logx.Error(utils.PrintObj(info))
		return false, errors.New("observeCall ServiceName or Topic is empty")
	}
	if info.TargetAddress == "" || info.TargetPort == 0 || info.TargetService == "" || info.TargetPath == "" {
		logx.Error(utils.PrintObj(info))
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
			//TODO:网络错误，进入重试流程，超过重试次数，返回错误
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
		//TODO:网络错误，应该进入重试流程，未实现
		if err != nil {
			logx.Errorf("CallService 网络错误 Payload:%s ,Error:%s", utils.PrintObj(payload), err.Error())
			return nil, err
		}
		err = json.Unmarshal(values, res)
		if err != nil {
			logx.Errorf("CallService 数据错误 Payload:%s, Values:%s ,Error:%s", utils.PrintObj(payload), string(values), err.Error())
			return nil, err
		}
	}
	return res, nil
}

// sendPayload dispatches a payload. When a TransportSelector is configured,
// it is used for all payloads regardless of TargetAddress, allowing
// service-discovery-resolved targets to be used by the selector.
func (own *ServiceContext) sendPayload(ctx context.Context, payload *types.PayLoad) ([]byte, error) {
	if own.TransportSelector != nil {
		target := payload.TargetAddress
		if payload.TargetPort > 0 {
			target = target + ":" + strconv.Itoa(payload.TargetPort)
		}
		result, err := transport.SendWithFallback(ctx, own.TransportSelector, payload, target)
		if err == nil {
			return result, nil
		}
		logx.Errorf("transport selector failed (%v), falling back to HTTP CallService", err)
	}
	return own.Service.CallService(payload)
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
