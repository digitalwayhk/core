package router

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/yitter/idgenerator-go/idgen"
	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config    *config.ServerConfig
	Service   *types.Service
	snow      idgen.ISnowWorker
	Router    *ServiceRouter
	isStart   bool
	Pid       int
	Hub       interface{} `json:"-"`
	StateChan chan bool   `json:"-"`
}

const DEFAULTPORT = 8080
const DEFAULTSOCKETPORT = 7070

var scontext map[string]*ServiceContext

func init() {
	scontext = make(map[string]*ServiceContext)
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
		if _, ok := service.AttachService[sr.ServiceName]; !ok {
			service.AttachService[sr.ServiceName] = &types.ServiceAttach{
				ServiceName:     sr.ServiceName,
				ObserverRouters: make(map[string]*types.ObserveArgs),
			}
		}
		as := service.AttachService[sr.ServiceName]
		as.ObserverRouters[sr.Topic] = sr
	}
	req := &InitRequest{}
	for _, cs := range service.Routers {
		safedo(cs, req)
	}
	if req.CallRouters != nil {
		for path, cr := range req.CallRouters {
			cinfo := cr.RouterInfo()
			sname := cinfo.ServiceName
			if _, ok := service.AttachService[sname]; !ok {
				service.AttachService[sname] = &types.ServiceAttach{
					ServiceName: sname,
					CallRouters: make(map[string]types.IRouter),
				}
			}
			as := service.AttachService[sname]
			if as.CallRouters == nil {
				as.CallRouters = make(map[string]types.IRouter)
			}
			as.CallRouters[path] = cr
		}
	}
	return service
}
func safedo(cs types.IRouter, req types.IRequest) {
	defer func() {
		if recover() != nil {
			// 一个函数的返回结果可以在defer调用中修改。
		}
	}()
	cs.Validation(req)
	cs.Do(req)
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
	own.StateChan <- state
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
			if cas.SocketPort == 0 {
				csc := own.GetServerConfig(as.Address, as.Port)
				if csc != nil {
					as.SocketPort = csc.SocketPort
					cas.SocketPort = csc.SocketPort
					cas.Address = csc.RunIp
					as.Address = csc.RunIp
					own.Config.Save()
				}
			}
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
	//as := own.Service.AttachService[oa.ServiceName]
	//if as == nil {
	//if _, ok := as.ObserverRouters[oa.Topic]; !ok {
	ok, err := own.observeCall(oa, info)
	if err != nil {
		return err
	}
	//as.IsAttach = ok
	oa.IsOk = ok
	//as.ObserverRouters[oa.Topic] = oa
	//}
	//}
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
			ti.TargetPath = info.Path
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
		UserId:           0,
		ClientIP:         oa.OwnAddress,
		Auth:             false,
		Instance:         oa,
	}
	// if info.Router != nil {
	// 	payload.Instance = info.Router
	// }
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
		TargetPath:       info.Path,
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
			return nil, err
		}
		json.Unmarshal(values, res)
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
