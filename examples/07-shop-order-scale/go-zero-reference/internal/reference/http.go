// 本文件直接用 go-zero REST 和 JWT 库实现买家下单、本人订单查询及就绪探针。
package reference

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

func (a *application) newHTTP() (*rest.Server, error) {
	c := a.file.RestConf
	c.Host = "127.0.0.1"
	c.Port = a.c.HTTPPort
	c.Prometheus.Port = 0
	c.Middlewares.Log = false
	if a.c.Role == "user" {
		c.Timeout = 0
	}
	s, e := rest.NewServer(c)
	if e != nil {
		return nil, e
	}
	// 与现有 Core 的安全访问日志保持相同 I/O 级别，不记录 body 或 token。
	s.Use(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			m := httpsnoop.CaptureMetrics(http.HandlerFunc(next), w, r)
			logx.Infow("http_request_completed", logx.Field("service", c.Name), logx.Field("route", r.URL.Path), logx.Field("method", r.Method), logx.Field("status", m.Code), logx.Field("duration_ms", m.Duration.Milliseconds()), logx.Field("response_bytes", m.Written))
		}
	})
	s.AddRoute(rest.Route{Method: http.MethodGet, Path: "/api/ready", Handler: func(w http.ResponseWriter, r *http.Request) {
		writeResponse(w, 200, map[string]bool{"ready": true}, nil)
	}})
	if a.c.Role == "user" {
		s.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/shop-user/addorder", Handler: a.addOrder})
		s.AddRoute(rest.Route{Method: http.MethodPost, Path: "/api/shop-user/getorders", Handler: a.getOrders})
	}
	return s, nil
}

func (a *application) user(r *http.Request) (uint, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return 0, errors.New("需要用户 Access Token")
	}
	t, e := jwt.Parse(strings.TrimPrefix(header, "Bearer "), func(t *jwt.Token) (interface{}, error) { return []byte(a.file.Auth.AccessSecret), nil }, jwt.WithValidMethods([]string{"HS256"}))
	if e != nil || !t.Valid {
		return 0, errors.New("用户 Token 无效")
	}
	c, ok := t.Claims.(jwt.MapClaims)
	if !ok || c["token_use"] != "access" || c["auth_type"] != "auth" {
		return 0, errors.New("Token 认证域无效")
	}
	uid, _ := c["uid"].(string)
	id, e := strconv.ParseUint(uid, 10, 64)
	if e != nil || id == 0 {
		return 0, errors.New("用户身份无效")
	}
	return uint(id), nil
}

func (a *application) addOrder(w http.ResponseWriter, r *http.Request) {
	user, e := a.user(r)
	if e != nil {
		writeResponse(w, 401, nil, e)
		return
	}
	var q OrderRequest
	if e = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q); e != nil {
		writeResponse(w, 400, nil, e)
		return
	}
	if e = q.validate(); e != nil {
		writeResponse(w, 400, nil, e)
		return
	}
	trace := uuid.NewString()
	products, e := a.productRPC(r.Context(), q.ProductID)
	if e != nil {
		writeResponse(w, 500, nil, e)
		return
	}
	if len(products) == 0 {
		writeResponse(w, 404, nil, errors.New("商品不存在或不可下单"))
		return
	}
	p := products[0]
	q.UserID = user
	q.SupplierID = uint(p.SupplierId)
	q.SupplierCode = p.SupplierCode
	q.SupplierName = p.SupplierName
	q.ProductCode = p.Code
	q.ProductName = p.Name
	q.UnitPrice, e = decimal.NewFromString(p.Price)
	if e != nil {
		writeResponse(w, 500, nil, e)
		return
	}
	q.OrderID, e = a.requestID(user, q.RequestID)
	if e != nil {
		writeResponse(w, 500, nil, e)
		return
	}
	order, e := a.createRPC(r.Context(), &q, trace)
	if e != nil {
		writeResponse(w, 500, nil, e)
		return
	}
	writeResponse(w, 200, order, nil)
}

func (a *application) getOrders(w http.ResponseWriter, r *http.Request) {
	user, e := a.user(r)
	if e != nil {
		writeResponse(w, 401, nil, e)
		return
	}
	orders, e := a.ordersRPC(r.Context(), user)
	if e != nil {
		writeResponse(w, 500, nil, e)
		return
	}
	writeResponse(w, 200, orders, nil)
}

func writeResponse(w http.ResponseWriter, status int, data interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	message := ""
	if err != nil {
		message = "internal server error"
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": err == nil, "errorCode": status, "errorMessage": message, "data": data})
}
