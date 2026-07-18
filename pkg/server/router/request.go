package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/safe"
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
	userID        string
	userName      string
	clientIP      string
	apiPath       string
	startTime     time.Time
	auth          bool
	http          *http.Request
	service       *ServiceContext
	servicerouter *ServiceRouter
	routerinfo    *types.RouterInfo
	secretClaims  *requestSecretClaims
}

type requestSecretClaims struct {
	values map[string]string
}

func getRequestInfo(r *http.Request, req *Request) {
	if r == nil || req == nil {
		return
	}
	url := strings.Trim(r.RequestURI, " ")
	path := strings.Split(url, "?")[0]
	req.apiPath = path
	req.http = r
	trustedProxies := []string(nil)
	if req.service != nil && req.service.Config != nil {
		trustedProxies = req.service.Config.TrustedProxies
	}
	req.clientIP = utils.ClientPublicIP(r, trustedProxies...)
	ctext := r.Context()
	obj := ctext.Value("uid")
	if obj != nil {
		suid := obj.(string)
		req.userID = suid
	}
	nobj := ctext.Value("uname")
	if nobj != nil {
		uname := ctext.Value("uname").(string)
		req.userName = uname
	}
	req.traceID = getTraceID(ctext, r)
	req.SetSecretClaims(safe.VerifiedSecretClaimsFromContext(ctext))
	//logx.Infof("api: %s, traceID: %s", req.apiPath, req.traceID)
}

// GetSecretClaim 返回当前已验签请求的服务端秘密 Claim。
func (own *Request) GetSecretClaim(key string) (string, bool) {
	if own == nil || own.secretClaims == nil || own.secretClaims.values == nil {
		return "", false
	}
	value, ok := own.secretClaims.values[key]
	return value, ok
}

// SetSecretClaims 仅供框架认证边界注入已验签并解密的 Claim 快照。
func (own *Request) SetSecretClaims(claims map[string]string) {
	if own == nil {
		return
	}
	if len(claims) == 0 {
		own.secretClaims = nil
		return
	}
	own.secretClaims = &requestSecretClaims{values: types.CloneSecretClaims(claims)}
}
func getUserIDAndName(req *Request, r *http.Request) (string, string) {
	if r == nil {
		return "", ""
	}
	ctext := r.Context()
	uid := ""
	uname := ""
	obj := ctext.Value("uid")
	if obj != nil {
		uid = obj.(string)
	}
	nobj := ctext.Value("uname")
	if nobj != nil {
		uname = ctext.Value("uname").(string)
	}
	req.userID = uid
	req.userName = uname
	return uid, uname
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
			req.auth = info.GetAuth()
			req.routerinfo = info
			getUserIDAndName(req, r)
			if req.auth {
				if req.userID == "" && req.userName == "" {
					logx.Errorf("Auth required but no user info found for api: %s", req.apiPath)
					return nil
				}
				logx.Infof("Auth required for api: %s", req.apiPath)
			}
		}
	}
	return req
}
func getTraceID(ctx context.Context, r *http.Request) string {
	if r != nil {
		if traceID := r.Header.Get("X-Trace-Id"); traceID != "" {
			//logx.Infof("api: %s, 获取到X-Trace-Id-internal: %s", r.RequestURI, traceID)
			return traceID
		}
	}
	// 1. 优先从OpenTelemetry获取
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}

	// 4. 生成新的traceID
	uid, _ := uuid.NewV4()
	return uid.String()
}
func (own *Request) GetHttpRequest() *http.Request {
	return own.http
}
func (own *Request) GetTraceId() string {
	return own.traceID
}
func (own *Request) ClearTraceId() {
	own.traceID = ""
	own.startTime = time.Now()
}
func (own *Request) GetPath() string {
	return own.apiPath
}
func (own *Request) GetUser() (string, string) {
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
	val := own.getValue(key)
	if val == "" {
		key = strings.Replace(key, "-", "", -1)
		val = own.getValue(key)
	}
	return val
}
func (own *Request) getValue(key string) string {
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
	sourceType := sinfo.GetPathType()
	targetType := tinfo.GetPathType()
	if sourceType != targetType {
		if sourceType != types.ManageType {
			if sourceType == types.PublicType {
				if targetType != types.PublicType {
					return errors.New("不能调用目标路由,public 路由只能调用 public type的路由!")
				}
			}
			if sourceType == types.PrivateType {
				if targetType == types.ManageType {
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
		TargetAddress: cont.Config.RunIp,
		TargetService: own.ServiceName(),
		TargetPort:    cont.Config.Port,
	}
}
func (own *Request) GetTargetServerInfo(serviceName string) *types.TargetInfo {
	if own != nil && own.service != nil && own.service.ServiceResolver != nil {
		resolved, err := own.service.ServiceResolver.Resolve(context.Background(), serviceName)
		if err == nil {
			return resolved.Info
		}
		return nil
	}
	cont := GetContext(serviceName)
	if cont == nil {
		return nil
	}
	return &types.TargetInfo{
		TargetAddress: cont.Config.Host,
		TargetService: serviceName,
		TargetPort:    cont.Config.Port,
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
	return own.service.CallService(payload, callback...)
}
func (own *Request) callPayload(router types.IRouter) (*types.PayLoad, error) {
	sinfo := own.servicerouter.GetRouter(own.apiPath)
	tinfo := router.RouterInfo()
	err := callrouterpermissions(sinfo, tinfo)
	if err != nil {
		return nil, err
	}
	return ToPayLoad(own, router, tinfo), nil
}
func GetPayLoad(traceid, sourceservice, sourcepath, uname string, uid string, router types.IRouter) *types.PayLoad {
	info := router.RouterInfo()
	return &types.PayLoad{
		TraceID:       traceid,
		SourceService: sourceservice,
		SourcePath:    sourcepath,
		TargetService: info.GetServiceName(),
		TargetPath:    info.GetPath(),
		UserId:        uid,
		UserName:      uname,
		ClientIP:      utils.GetLocalIP(),
		Auth:          false,
		Instance:      router,
		HttpMethod:    info.GetMethod(),
	}
}

func ToPayLoad(req *Request, router types.IRouter, tinfo *types.RouterInfo) *types.PayLoad {
	uid, uname := req.GetUser()
	info := router.RouterInfo()
	return &types.PayLoad{
		TraceID:       req.GetTraceId(),
		SourceService: req.ServiceName(),
		SourcePath:    req.GetPath(),
		TargetService: info.GetServiceName(),
		TargetPath:    info.GetPath(),
		UserId:        uid,
		UserName:      uname,
		ClientIP:      req.GetClientIP(),
		Auth:          req.Authorized(),
		Instance:      router,
		HttpMethod:    tinfo.GetMethod(),
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
	req.auth = info.GetAuth()
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
	own.CallRouters[info.GetPath()] = router
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
