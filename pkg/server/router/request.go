package router

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/gofrs/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
	"go.opentelemetry.io/otel/trace"
)

type Request struct {
	traceID       string
	host          string
	userID        uint
	userName      string
	clientIP      string
	apiPath       string
	startTime     time.Time
	auth          bool
	http          *http.Request
	service       *ServiceContext
	servicerouter *ServiceRouter
	routerinfo    *types.RouterInfo
}

func getRequestInfo(r *http.Request, req *Request) {
	if r == nil || req == nil {
		return
	}
	url := strings.Trim(r.RequestURI, " ")
	path := strings.Split(url, "?")[0]
	req.apiPath = path
	req.http = r
	req.clientIP = utils.ClientPublicIP(r)
	var uid int64
	var uname string
	ctext := r.Context()
	obj := ctext.Value("uid")
	if obj != nil {
		uid, _ = obj.(json.Number).Int64()
	}
	nobj := ctext.Value("uname")
	if nobj != nil {
		uname = ctext.Value("uname").(string)
	}
	req.userID = uint(uid)
	req.userName = uname
	spanCtx := trace.SpanContextFromContext(ctext)
	tid := ""
	if spanCtx.HasTraceID() {
		tid = spanCtx.TraceID().String()
	}
	req.traceID = tid
}

// NewRequest 路由接收到请求
func NewRequest(routers *ServiceRouter, r *http.Request) *Request {
	req := &Request{
		servicerouter: routers,
		startTime:     time.Now(),
		auth:          false,
	}
	if routers != nil && routers.Service != nil {
		req.service = routers.Service
		req.host = routers.Service.Config.Host
	}
	if r != nil && routers != nil {
		getRequestInfo(r, req)
		info := routers.GetRouter(req.apiPath)
		if info != nil {
			req.auth = info.Auth
			req.routerinfo = info
		}
	}
	return req
}

func (own *Request) GetTraceId() string {
	if own.traceID == "" {
		uid, _ := uuid.NewV4()
		own.traceID = uid.String()
	}
	return own.traceID
}
func (own *Request) ClearTraceId() {
	own.traceID = ""
	own.startTime = time.Now()
}
func (own *Request) GetPath() string {
	return own.apiPath
}
func (own *Request) GetUser() (uint, string) {
	return own.userID, own.userName
}
func (own *Request) GetClientIP() string {
	return own.clientIP
}
func (own *Request) NewID() uint {
	return own.service.NewID()
}
func (own *Request) Authorized() bool {
	return own.auth
}
func (own *Request) GetValue(key string) string {
	val := own.http.FormValue(key)
	if val == "" {
		query := own.http.URL.Query()
		val = query.Get(key)
		if val == "" {
			for k, v := range query {
				if strings.EqualFold(k, key) {
					val = v[0]
				}
			}
		}
	}
	return val
}
func (own *Request) GetClaims(key string) interface{} {
	if own.http == nil {
		return nil
	}
	return own.http.Context().Value(key)
}
func (own *Request) SetPath(path string) {
	own.apiPath = path
}
func (own *Request) ServiceName() string {
	return own.service.Service.Name
}

const maxBodyLen int64 = 8388608

func (own *Request) Bind(v interface{}) error {
	r := own.http
	if r.Body == http.NoBody {
		return nil
	}
	reader := io.LimitReader(r.Body, maxBodyLen)
	var buf strings.Builder
	teeReader := io.TeeReader(reader, &buf)
	decoder := json.NewDecoder(teeReader)
	return decoder.Decode(v)
}
func (own *Request) GoZeroBind(v interface{}) error {
	return httpx.ParseJsonBody(own.http, v)
}
func (own *Request) GetService() *ServiceContext {
	return own.service
}

func callrouterpermissions(sinfo, tinfo *types.RouterInfo) error {
	if sinfo.PathType != tinfo.PathType {
		if sinfo.PathType != types.ManageType {
			if sinfo.PathType == types.PublicType {
				if tinfo.PathType != types.PublicType {
					return errors.New("不能调用目标路由,public 路由只能调用 public type的路由!")
				}
			}
			if sinfo.PathType == types.PrivateType {
				if tinfo.PathType == types.ManageType {
					return errors.New("不能调用目标路由,manage 路由只能由调用 manage type路由调用!")
				}
			}
		}
	}
	return nil
}
func (own *Request) GetServerInfo() *types.TargetInfo {
	cont := own.GetService()
	return &types.TargetInfo{
		TargetAddress:    cont.Config.Host,
		TargetService:    own.ServiceName(),
		TargetPort:       cont.Config.Port,
		TargetSocketPort: cont.Config.SocketPort,
	}
}
func (own *Request) GetTargetServerInfo(serviceName string) *types.TargetInfo {
	cont := GetContext(serviceName)
	if cont == nil {
		return nil
	}
	return &types.TargetInfo{
		TargetAddress:    cont.Config.Host,
		TargetService:    serviceName,
		TargetPort:       cont.Config.Port,
		TargetSocketPort: cont.Config.SocketPort,
	}
}
func (own *Request) CallService(router types.IRouter, callback ...func(res types.IResponse)) (types.IResponse, error) {
	return own.CallTargetService(router, nil, callback...)
}
func (own *Request) CallTargetService(router types.IRouter, info *types.TargetInfo, callback ...func(res types.IResponse)) (types.IResponse, error) {
	payload, err := own.callPayload(router)
	if err != nil {
		return nil, err
	}
	if utils.IsTest() {
		err := router.Validation(own)
		if err != nil {
			return own.NewResponse(err, nil), nil
		}
		rest := own.NewResponse(router.Do(own))
		if callback != nil {
			callback[0](rest)
		}
		return rest, nil
	}
	if info != nil {
		if info.TargetAddress == "" || info.TargetPort == 0 {
			return nil, errors.New("目标地址或端口错误")
		}
		payload.TargetAddress = info.TargetAddress
		payload.TargetPort = info.TargetPort
		if payload.TargetService == "" && info.TargetService != "" {
			payload.TargetService = info.TargetService
		}
		if payload.TargetPath == "" && info.TargetPath != "" {
			payload.TargetPath = info.TargetPath
		}
		if info.TargetSocketPort == 0 {
			con := GetContext(own.ServiceName()).GetServerConfig(info.TargetAddress, info.TargetPort)
			payload.TargetSocketPort = con.SocketPort
		} else {
			payload.TargetSocketPort = info.TargetSocketPort
		}
	}
	return own.service.CallService(payload, callback...)
}
func (own *Request) callPayload(router types.IRouter) (*types.PayLoad, error) {
	sinfo := own.servicerouter.GetRouter(own.apiPath)
	tinfo := router.RouterInfo()
	err := callrouterpermissions(sinfo, tinfo)
	if err != nil {
		return nil, err
	}
	return ToPayLoad(own, router), nil
}
func GetPayLoad(traceid, sourceservice, sourcepath, uname string, uid uint, router types.IRouter) *types.PayLoad {
	info := router.RouterInfo()
	return &types.PayLoad{
		TraceID:       traceid,
		SourceService: sourceservice,
		SourcePath:    sourcepath,
		TargetService: info.ServiceName,
		TargetPath:    info.Path,
		UserId:        uid,
		UserName:      uname,
		ClientIP:      utils.GetLocalIP(),
		Auth:          false,
		Instance:      router,
	}
}
func ToPayLoad(req *Request, router types.IRouter) *types.PayLoad {
	uid, uname := req.GetUser()
	info := router.RouterInfo()
	return &types.PayLoad{
		TraceID:       req.GetTraceId(),
		SourceService: req.ServiceName(),
		SourcePath:    req.GetPath(),
		TargetService: info.ServiceName,
		TargetPath:    info.Path,
		UserId:        uid,
		UserName:      uname,
		ClientIP:      req.GetClientIP(),
		Auth:          req.Authorized(),
		Instance:      router,
	}
}

func ToRequest(own *types.PayLoad) types.IRequest {
	req := &Request{
		traceID:   own.TraceID,
		host:      own.TargetAddress,
		userID:    own.UserId,
		userName:  own.UserName,
		clientIP:  own.ClientIP,
		apiPath:   own.TargetPath,
		startTime: time.Now(),
		auth:      own.Auth,
	}
	req.service = GetContext(own.TargetService)
	if req.service == nil {
		logx.Error("服务不存在", own.TargetService)
		return nil
	}
	req.servicerouter = req.service.Router
	info := req.servicerouter.GetRouter(req.apiPath)
	req.auth = info.Auth
	req.routerinfo = info
	return req
}

var snow = utils.NewAlgorithmSnowFlake(1000, 1000)

type InitRequest struct {
	Request
	CallRouters map[string]types.IRouter
}

func (own *InitRequest) CallService(router types.IRouter, callback ...func(res types.IResponse)) (types.IResponse, error) {
	if own.CallRouters == nil {
		own.CallRouters = make(map[string]types.IRouter)
	}
	info := router.RouterInfo()
	own.CallRouters[info.Path] = router
	return &Response{Success: false}, nil
}

func (own *InitRequest) NewResponse(data interface{}, err error) types.IResponse {
	suc := true
	msg := ""
	if err != nil {
		suc = false
		msg = err.Error()
	}
	res := &Response{
		err:          err,
		TraceID:      own.GetTraceId(),
		ErrorCode:    200,
		ErrorMessage: msg,
		Data:         data,
		Success:      suc,
		Duration:     time.Since(own.startTime),
		Host:         "testing env",
	}
	return res
}
func (own *InitRequest) GetTraceId() string {
	uid, _ := uuid.NewV4()
	return uid.String()
}

func (own *InitRequest) NewID() uint {
	return uint(snow.NextId())
}
