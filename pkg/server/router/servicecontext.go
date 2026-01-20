package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/yitter/idgenerator-go/idgen"
	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config       *config.ServerConfig
	Service      *types.Service
	snow         idgen.ISnowWorker
	Router       *ServiceRouter
	isStart      bool
	Pid          int
	Hub          interface{} `json:"-"`
	StateChan    chan bool   `json:"-"`
	serverOption *types.ServerOption
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
	sc.snow = utils.NewAlgorithmSnowFlake(con.MachineID, con.DataCenterID)
	sc.Router = NewServiceRouter(sc, service)
	scontext[name] = sc
	return scontext[name]
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
	serviceName := req.ServiceName()
	path := req.GetPath()
	err := cs.Validation(req)
	if err != nil {
		logx.Error(fmt.Sprintf("服务%s的路由%s验证失败:%s", serviceName, path, err.Error()))
	}
	data, err := cs.Do(req)
	if err != nil {
		logx.Error(fmt.Sprintf("服务%s的路由%s执行失败:%s", serviceName, path, err.Error()))
	}
	info := cs.RouterInfo()
	TestResult[info.GetPath()] = data
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
	// 🔧 非阻塞发送，避免死锁
	select {
	case own.StateChan <- state:
		// 发送成功
	default:
		// 通道满了，在当前架构下这不是问题
		logx.Debugf("StateChan已满，跳过状态通知: %s", own.Service.Name)
	}
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
			values, err := own.Service.CallService(payload)
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
		values, err := own.Service.CallService(payload)
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
