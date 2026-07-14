package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Suite 保存一次集成测试进程共享的服务地址和子进程资源。
type Suite struct {
	RootDir      string
	BaseURL      string
	WebSocketURL string
	command      *exec.Cmd
	outputFile   *os.File
}

// ResponseEnvelope 对应框架默认 HTTP 响应外壳。
type ResponseEnvelope struct {
	HTTPStatus   int             `json:"-"`
	Body         string          `json:"-"`
	Success      bool            `json:"success"`
	ErrorCode    int             `json:"errorCode"`
	ErrorMessage string          `json:"errorMessage"`
	Data         json.RawMessage `json:"data"`
}

// ProductDTO 是集成测试关注的商品公开字段。
type ProductDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price string `json:"price"`
}

// OrderDTO 是集成测试关注的订单公开字段。
type OrderDTO struct {
	ID          string `json:"id"`
	ProductID   uint   `json:"productID"`
	ProductName string `json:"productName"`
	UnitPrice   string `json:"unitPrice"`
	Quantity    int    `json:"quantity"`
	UserID      string `json:"userID"`
	CreatedAt   string `json:"createdAt"`
}

// StartShopSuite 构建示例二进制，写入隔离配置并等待 HTTP 服务可用。
func StartShopSuite() (*Suite, error) {
	repoRoot, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	rootDir, err := os.MkdirTemp("", "core-simple-shop-")
	if err != nil {
		return nil, fmt.Errorf("创建测试目录: %w", err)
	}
	serverPort, err := freePort()
	if err != nil {
		return nil, err
	}
	shopPort, err := freePort()
	if err != nil {
		return nil, err
	}
	if err := writeServiceConfig(rootDir, "server", serverPort, 1); err != nil {
		return nil, err
	}
	if err := writeServiceConfig(rootDir, "shop", shopPort, 2); err != nil {
		return nil, err
	}

	binary := filepath.Join(rootDir, "simple-shop")
	build := exec.Command("go", "build", "-race", "-o", binary, "./examples/01-simple-shop/main")
	build.Dir = repoRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		return nil, fmt.Errorf("构建商城示例失败: %w\n%s", buildErr, output)
	}

	outputFile, err := os.Create(filepath.Join(rootDir, "service.log"))
	if err != nil {
		return nil, fmt.Errorf("创建服务日志: %w", err)
	}
	command := exec.Command(binary, "-view", "0")
	command.Dir = rootDir
	command.Stdout = outputFile
	command.Stderr = outputFile
	if err := command.Start(); err != nil {
		_ = outputFile.Close()
		return nil, fmt.Errorf("启动商城示例: %w", err)
	}

	created := &Suite{
		RootDir:      rootDir,
		BaseURL:      "http://127.0.0.1:" + strconv.Itoa(shopPort),
		WebSocketURL: "ws://127.0.0.1:" + strconv.Itoa(shopPort) + "/ws",
		command:      command,
		outputFile:   outputFile,
	}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	return created, nil
}

// repositoryRoot 根据当前测试文件定位仓库根目录，避免依赖调用者工作目录。
func repositoryRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("无法定位集成测试文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

// freePort 向系统申请一个暂时可用的本地端口。
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("申请测试端口: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// writeServiceConfig 使用框架默认值生成可独立运行的本地测试配置。
func writeServiceConfig(rootDir, name string, port, machineID int) error {
	serviceConfig := config.NewServiceDefaultConfig(name, port)
	serviceConfig.Host = "127.0.0.1"
	serviceConfig.RunIp = "127.0.0.1"
	serviceConfig.SocketPort = port + 10000
	serviceConfig.MachineID = uint(machineID)
	serviceConfig.DataCenterID = 1
	serviceConfig.Telemetry.Disabled = true
	serviceConfig.Log.Mode = "console"
	serviceConfig.Cluster.Mode = "auto"
	serviceConfig.Cluster.Provider = "local"
	serviceConfig.MQ.Mode = "off"
	serviceConfig.MQ.Usage = nil
	serviceConfig.Transport.Internal = "grpc"
	serviceConfig.Transport.GRPC.Enable = false
	data, err := marshalServiceConfig(serviceConfig)
	if err != nil {
		return fmt.Errorf("编码 %s 配置: %w", name, err)
	}
	configDir := filepath.Join(rootDir, "etc")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, name+".json"), data, 0o600); err != nil {
		return fmt.Errorf("写入 %s 配置: %w", name, err)
	}
	return nil
}

// marshalServiceConfig 将所有 time.Duration 写成 go-zero 配置要求的字符串。
func marshalServiceConfig(serviceConfig *config.ServerConfig) ([]byte, error) {
	data, err := json.Marshal(serviceConfig)
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	stringifyDurations(reflect.ValueOf(serviceConfig).Elem(), values)
	return json.MarshalIndent(values, "", "  ")
}

// stringifyDurations 按结构体字段递归修正 JSON map 中的 duration 值。
func stringifyDurations(value reflect.Value, values map[string]interface{}) {
	typeOfDuration := reflect.TypeOf(time.Duration(0))
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := valueType.Field(index)
		if !field.IsExported() {
			continue
		}
		fieldValue := value.Field(index)
		fieldType := field.Type
		jsonKey := field.Name
		if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag != "" && tag != "-" {
			jsonKey = tag
		}
		if fieldType == typeOfDuration {
			values[jsonKey] = time.Duration(fieldValue.Int()).String()
			continue
		}
		if fieldType.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				continue
			}
			fieldValue = fieldValue.Elem()
			fieldType = fieldValue.Type()
		}
		if fieldType.Kind() != reflect.Struct || fieldType == reflect.TypeOf(time.Time{}) {
			continue
		}
		nested, ok := values[jsonKey].(map[string]interface{})
		if field.Anonymous {
			nested = values
			ok = true
		}
		if ok {
			stringifyDurations(fieldValue, nested)
		}
	}
}

// waitReady 轮询认证、商品和订单路由，确认 HTTP 与延迟数据表均已就绪。
func (s *Suite) waitReady() error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tokenResponse, err := http.Get(s.BaseURL + "/api/servermanage/testtoken?userid=health")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var tokenEnvelope ResponseEnvelope
		_ = json.NewDecoder(tokenResponse.Body).Decode(&tokenEnvelope)
		_ = tokenResponse.Body.Close()
		var token string
		_ = json.Unmarshal(tokenEnvelope.Data, &token)
		if tokenResponse.StatusCode != http.StatusOK || !tokenEnvelope.Success || token == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		productsResponse, err := http.Get(s.BaseURL + "/api/shop/getproducts")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var productsEnvelope ResponseEnvelope
		_ = json.NewDecoder(productsResponse.Body).Decode(&productsEnvelope)
		_ = productsResponse.Body.Close()
		if productsResponse.StatusCode != http.StatusOK || !productsEnvelope.Success {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		ordersRequest, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/shop/getorders", nil)
		if err != nil {
			return err
		}
		ordersRequest.Header.Set("Authorization", "Bearer "+token)
		ordersResponse, err := http.DefaultClient.Do(ordersRequest)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var ordersEnvelope ResponseEnvelope
		_ = json.NewDecoder(ordersResponse.Body).Decode(&ordersEnvelope)
		_ = ordersResponse.Body.Close()
		if ordersResponse.StatusCode == http.StatusOK && ordersEnvelope.Success {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Join(s.RootDir, "service.log"))
	return fmt.Errorf("等待商城服务启动超时\n%s", data)
}

// Stop 先请求优雅退出，超时后强制结束子进程并清理临时文件。
func (s *Suite) Stop() {
	if s == nil {
		return
	}
	if s.command != nil && s.command.Process != nil {
		_ = s.command.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_ = s.command.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = s.command.Process.Kill()
			<-done
		}
	}
	if s.outputFile != nil {
		_ = s.outputFile.Close()
	}
	_ = os.RemoveAll(s.RootDir)
}

// PrintLog 在测试失败时输出子进程日志。
func (s *Suite) PrintLog() {
	if s == nil {
		return
	}
	if data, err := os.ReadFile(filepath.Join(s.RootDir, "service.log")); err == nil {
		fmt.Fprintf(os.Stderr, "\n--- 商城集成测试服务日志 ---\n%s\n", data)
	}
}

// ValidateServiceConfigs 验证指定示例的静态服务配置可通过当前配置契约。
func ValidateServiceConfigs(t *testing.T, example string, serviceNames ...string) {
	t.Helper()
	repoRoot, err := repositoryRoot()
	require.NoError(t, err)
	for _, name := range serviceNames {
		name := name
		t.Run("config-"+name, func(t *testing.T) {
			path := filepath.Join(repoRoot, "examples", example, "main", "etc", name+".json")
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			var serviceConfig config.ServerConfig
			require.NoError(t, json.Unmarshal(data, &serviceConfig))
			serviceConfig.ApplyDefaults()
			require.NoError(t, serviceConfig.Validate())
		})
	}
}

// RequestJSON 发起 HTTP 请求并解析框架默认响应外壳。
func (s *Suite) RequestJSON(t *testing.T, method, path, token string, body interface{}) ResponseEnvelope {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, s.BaseURL+path, reader)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	envelope := ResponseEnvelope{HTTPStatus: response.StatusCode, Body: string(data)}
	if err := json.Unmarshal(data, &envelope); err != nil {
		require.NotEqual(t, http.StatusOK, response.StatusCode, string(data))
	}
	return envelope
}

// TokenFor 通过框架内建 TestToken 路由获取指定用户类型的令牌。
func (s *Suite) TokenFor(t *testing.T, userID string, tokenType int) string {
	t.Helper()
	path := "/api/servermanage/testtoken?userid=" + userID
	if tokenType != 0 {
		path += "&type=" + strconv.Itoa(tokenType)
	}
	envelope := s.RequestJSON(t, http.MethodGet, path, "", nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var token string
	require.NoError(t, json.Unmarshal(envelope.Data, &token))
	require.NotEmpty(t, token)
	return token
}

// AddProduct 通过真实 Manage Add 路由创建商品并返回公开字段。
func (s *Suite) AddProduct(t *testing.T, adminToken, name, price string) ProductDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]interface{}{
		"name":  name,
		"price": price,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var product ProductDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &product))
	require.NotEmpty(t, product.ID)
	return product
}

// UintID 将框架以字符串编码的模型 ID 转换为 uint。
func UintID(t *testing.T, id string) uint {
	t.Helper()
	value, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	require.NoError(t, err)
	return uint(value)
}

// WebSocketMessage 对应框架 WebSocket 的 event/channel/data 信封。
type WebSocketMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// OrderEvent 是用户订阅收到的订单变更消息。
type OrderEvent struct {
	Action string   `json:"action"`
	Order  OrderDTO `json:"order"`
}

// ConnectAndSubscribe 建立 WebSocket，使用 TestToken 登录并订阅本人订单。
func (s *Suite) ConnectAndSubscribe(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
	require.NoError(t, err)
	s.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	logon := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "success", logon.Event, string(logon.Data))
	require.Equal(t, "logon", logon.Channel)

	s.WriteWebSocket(t, connection, "sub", "/api/shop/getorders", map[string]interface{}{})
	subscribed := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, "/api/shop/getorders", subscribed.Channel)
	return connection
}

// WriteWebSocket 发送符合框架协议的 WebSocket 消息。
func (s *Suite) WriteWebSocket(t *testing.T, connection *websocket.Conn, event, channel string, data interface{}) {
	t.Helper()
	require.NoError(t, connection.WriteJSON(map[string]interface{}{
		"event":   event,
		"channel": channel,
		"data":    data,
	}))
}

// ReadWebSocket 在给定时限内读取并解析一条 WebSocket 消息。
func (s *Suite) ReadWebSocket(t *testing.T, connection *websocket.Conn, timeout time.Duration) WebSocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(timeout)))
	_, data, err := connection.ReadMessage()
	require.NoError(t, err)
	var message WebSocketMessage
	require.NoError(t, json.Unmarshal(data, &message), string(data))
	return message
}

// ReadOrderEvent 读取并解析当前用户的订单变更消息。
func (s *Suite) ReadOrderEvent(t *testing.T, connection *websocket.Conn) OrderEvent {
	t.Helper()
	message := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "/api/shop/getorders", message.Channel)
	var event OrderEvent
	require.NoError(t, json.Unmarshal(message.Data, &event), string(message.Data))
	return event
}

// StreamWebSocket 持续读取指定连接，使多次“没有消息”断言不会破坏连接状态。
func (s *Suite) StreamWebSocket(t *testing.T, connection *websocket.Conn) <-chan WebSocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Time{}))
	messages := make(chan WebSocketMessage, 4)
	go func() {
		defer close(messages)
		for {
			_, data, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var message WebSocketMessage
			if json.Unmarshal(data, &message) == nil {
				messages <- message
			}
		}
	}()
	return messages
}

// AssertNoOrderEvent 验证另一用户在短窗口内没有收到订单通知。
func AssertNoOrderEvent(t *testing.T, messages <-chan WebSocketMessage) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf("其他用户不应收到订单事件: %+v", message)
	case <-time.After(250 * time.Millisecond):
	}
}

// ProductNames 提取商品名称，简化列表断言。
func ProductNames(products []ProductDTO) []string {
	names := make([]string, 0, len(products))
	for _, product := range products {
		names = append(names, product.Name)
	}
	return names
}

// OrderIDs 提取订单 ID，简化所有权与删除结果断言。
func OrderIDs(orders []OrderDTO) []string {
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}
	return ids
}
