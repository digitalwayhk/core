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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Suite 保存一次集成测试进程共享的服务地址和子进程资源。
type Suite struct {
	RootDir      string
	BasePort     int
	GRPCBasePort int
	BaseURL      string
	WebSocketURL string
	Binary       string
	Arguments    []string
	Environment  map[string]string
	command      *exec.Cmd
	outputFile   *os.File
}

// ProcessOptions 描述一个与具体业务无关的示例进程启动方式。
type ProcessOptions struct {
	BuildPackage     string
	BinaryName       string
	TempPrefix       string
	ServiceCount     int
	ServiceIndex     int
	GRPCServiceCount int
	Arguments        []string
	Environment      map[string]string
	DisableRace      bool
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

// TokenResponse 对应框架 Callback、Refresh 和 TestToken 的结构化 Token 响应。
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenType        string `json:"token_type"`
	AccessExpiresIn  int64  `json:"access_expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
}

// TransportStatsSnapshot is the structured local server-management response
// used to prove which internal transport a real integration flow selected.
type TransportStatsSnapshot struct {
	Transport struct {
		GRPCSelected uint64
		HTTPSelected uint64
		SendSuccess  uint64
		SendFailure  uint64
		HTTPFallback uint64
		InboundGRPC  uint64
	} `json:"transport"`
	Fallback []string `json:"fallback"`
}

// AccessTokenFromData 优先解析结构化 Token 响应，并在过渡期兼容旧字符串响应。
func AccessTokenFromData(data json.RawMessage) (string, error) {
	var response TokenResponse
	if err := json.Unmarshal(data, &response); err == nil && strings.TrimSpace(response.AccessToken) != "" {
		return response.AccessToken, nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil && strings.TrimSpace(legacy) != "" {
		return legacy, nil
	}
	return "", fmt.Errorf("响应中缺少 access_token")
}

// SetJSONConfigLogLevel 仅用于已由框架生成的临时集成测试配置。
// benchmark 可用它关闭 info 级访问日志，避免把日志 I/O 误计为业务热路径成本。
func SetJSONConfigLogLevel(configPath, level string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取集成测试配置: %w", err)
	}
	var content map[string]interface{}
	if err := json.Unmarshal(data, &content); err != nil {
		return fmt.Errorf("解析集成测试配置: %w", err)
	}
	logConfig, _ := content["Log"].(map[string]interface{})
	if logConfig == nil {
		logConfig = make(map[string]interface{})
	}
	logConfig["Level"] = strings.TrimSpace(level)
	content["Log"] = logConfig
	encoded, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return fmt.Errorf("编码集成测试配置: %w", err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return fmt.Errorf("写入集成测试配置: %w", err)
	}
	return nil
}

// httpClient 复用连接，避免高并发 benchmark 因 DefaultTransport
// MaxIdleConnsPerHost=2 产生大量 TIME_WAIT 耗尽 ephemeral 端口。
// 连接上限覆盖示例性能报告的 1000 并发档位；普通集成测试只会按实际请求建立连接，
// 这里的容量不是预创建数量，也不会让低并发测试额外占用 2048 条连接。
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2048,
		MaxIdleConnsPerHost:   2048,
		MaxConnsPerHost:       2048,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

var processStartPortMu sync.Mutex

const defaultProcessReadyTimeout = 30 * time.Second

// WebSocketMessage 对应框架 WebSocket 的 event/channel/data 信封。
type WebSocketMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// IsBenchmarkRun 判断当前 go test 是否请求 benchmark，用于关闭子进程 race 插桩。
func IsBenchmarkRun() bool {
	for _, argument := range os.Args {
		if strings.HasPrefix(argument, "-test.bench=") && strings.TrimPrefix(argument, "-test.bench=") != "" {
			return true
		}
	}
	return false
}

// StartProcess 构建并启动示例进程，只处理通用进程、端口、日志和临时目录生命周期。
// 服务首次启动时由框架在临时运行目录自动生成配置文件。
func StartProcess(options ProcessOptions) (*Suite, error) {
	if options.BuildPackage == "" || options.BinaryName == "" {
		return nil, fmt.Errorf("构建包和二进制名称不能为空")
	}
	if options.ServiceCount <= 0 {
		options.ServiceCount = 1
	}
	if options.ServiceIndex < 0 || options.ServiceIndex >= options.ServiceCount {
		return nil, fmt.Errorf("服务索引 %d 超出服务数量 %d", options.ServiceIndex, options.ServiceCount)
	}
	if options.TempPrefix == "" {
		options.TempPrefix = "core-integration-"
	}

	repoRoot, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	rootDir, err := os.MkdirTemp("", options.TempPrefix)
	if err != nil {
		return nil, fmt.Errorf("创建测试目录: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(rootDir) }

	binary := filepath.Join(rootDir, options.BinaryName)
	buildArgs := []string{"build"}
	if !options.DisableRace {
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "-o", binary, options.BuildPackage)
	build := exec.Command("go", buildArgs...)
	build.Dir = repoRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		cleanup()
		return nil, fmt.Errorf("构建示例失败: %w\n%s", buildErr, output)
	}
	// Build first, then reserve ports immediately before process start. The child
	// cannot inherit these listeners, so this minimizes the probe-to-bind window.
	// 并发启动多个真实进程时，端口探测与子进程 bind 必须串行，否则两个
	// StartProcess 可能同时探测到同一段可用端口。
	processStartPortMu.Lock()
	defer processStartPortMu.Unlock()
	basePort, err := reservePortRange(options.ServiceCount)
	if err != nil {
		cleanup()
		return nil, err
	}
	grpcBasePort, err := reserveNonOverlappingPortRange(options.GRPCServiceCount, basePort, options.ServiceCount)
	if err != nil {
		cleanup()
		return nil, err
	}

	arguments := append([]string{}, options.Arguments...)
	arguments = append(arguments, "-p", strconv.Itoa(basePort))
	if grpcBasePort > 0 {
		arguments = append(arguments, "-grpc", strconv.Itoa(grpcBasePort))
	}
	suite := &Suite{
		RootDir: rootDir, BasePort: basePort, GRPCBasePort: grpcBasePort,
		BaseURL:      "http://127.0.0.1:" + strconv.Itoa(basePort+options.ServiceIndex),
		WebSocketURL: "ws://127.0.0.1:" + strconv.Itoa(basePort+options.ServiceIndex) + "/ws",
		Binary:       binary, Arguments: arguments, Environment: cloneEnvironment(options.Environment),
	}
	if err := suite.Restart(); err != nil {
		cleanup()
		return nil, err
	}
	if err := suite.waitBoundPorts(options.ServiceCount, options.GRPCServiceCount, defaultProcessReadyTimeout); err != nil {
		suite.StopProcess()
		logData, _ := os.ReadFile(filepath.Join(suite.RootDir, "service.log"))
		cleanup()
		if len(logData) != 0 {
			return nil, fmt.Errorf("%w\n--- 集成测试服务日志 ---\n%s", err, logData)
		}
		return nil, err
	}
	return suite, nil
}

// Restart 使用同一二进制、端口和运行目录重新启动服务。
func (s *Suite) Restart() error {
	if s == nil || s.Binary == "" {
		return fmt.Errorf("测试进程尚未完成构建")
	}
	outputFile, err := os.OpenFile(filepath.Join(s.RootDir, "service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("创建服务日志: %w", err)
	}
	command := exec.Command(s.Binary, s.Arguments...)
	command.Dir = s.RootDir
	command.Env = mergeEnvironment(os.Environ(), s.Environment)
	command.Stdout = outputFile
	command.Stderr = outputFile
	if err := command.Start(); err != nil {
		_ = outputFile.Close()
		return fmt.Errorf("启动示例: %w", err)
	}
	s.command = command
	s.outputFile = outputFile
	return nil
}

func (s *Suite) waitBoundPorts(httpCount, grpcCount int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.allPortsAccepting(httpCount, grpcCount) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("等待测试进程监听端口超时: httpBase=%d httpCount=%d grpcBase=%d grpcCount=%d", s.BasePort, httpCount, s.GRPCBasePort, grpcCount)
}

func (s *Suite) allPortsAccepting(httpCount, grpcCount int) bool {
	for offset := 0; offset < httpCount; offset++ {
		if !tcpPortAccepting(s.BasePort + offset) {
			return false
		}
	}
	for offset := 0; offset < grpcCount; offset++ {
		if !tcpPortAccepting(s.GRPCBasePort + offset) {
			return false
		}
	}
	return true
}

func tcpPortAccepting(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func cloneEnvironment(environment map[string]string) map[string]string {
	if len(environment) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(environment))
	for key, value := range environment {
		cloned[key] = value
	}
	return cloned
}

func mergeEnvironment(parent []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(parent)+len(overrides))
	for _, entry := range parent {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+merged[key])
	}
	return result
}

// repositoryRoot 根据当前测试文件定位仓库根目录，避免依赖调用者工作目录。
func repositoryRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("无法定位集成测试文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

// reservePortRange 获取一段连续可用端口，匹配 WebServer 多服务按序分配端口的规则。
func reservePortRange(count int) (int, error) {
	for attempt := 0; attempt < 100; attempt++ {
		first, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, fmt.Errorf("申请测试端口: %w", err)
		}
		basePort := first.Addr().(*net.TCPAddr).Port
		listeners := []net.Listener{first}
		available := basePort+count-1 <= 65535
		for offset := 1; available && offset < count; offset++ {
			listener, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(basePort+offset)))
			if listenErr != nil {
				available = false
				break
			}
			listeners = append(listeners, listener)
		}
		for _, listener := range listeners {
			_ = listener.Close()
		}
		if available {
			return basePort, nil
		}
	}
	return 0, fmt.Errorf("无法申请连续测试端口")
}

func reserveNonOverlappingPortRange(count, occupiedBase, occupiedCount int) (int, error) {
	if count <= 0 {
		return 0, nil
	}
	for attempt := 0; attempt < 100; attempt++ {
		base, err := reservePortRange(count)
		if err != nil {
			return 0, err
		}
		if base+count <= occupiedBase || occupiedBase+occupiedCount <= base {
			return base, nil
		}
	}
	return 0, fmt.Errorf("unable to reserve gRPC ports outside HTTP range")
}

// Stop 先请求优雅退出，超时后强制结束子进程并清理临时文件。
func (s *Suite) Stop() {
	if s == nil {
		return
	}
	s.StopProcess()
	_ = os.RemoveAll(s.RootDir)
}

// StopProcess 优雅停止子进程但保留配置、数据库和日志，供重启恢复测试使用。
func (s *Suite) StopProcess() {
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
	s.command = nil
	s.outputFile = nil
}

// KillProcess 强制终止子进程但保留运行目录，用于验证崩溃恢复。
func (s *Suite) KillProcess() {
	if s == nil {
		return
	}
	if s.command != nil && s.command.Process != nil {
		_ = s.command.Process.Kill()
		_ = s.command.Wait()
	}
	if s.outputFile != nil {
		_ = s.outputFile.Close()
	}
	s.command = nil
	s.outputFile = nil
}

// PrintLog 在测试失败时输出子进程日志。
func (s *Suite) PrintLog() {
	if s == nil {
		return
	}
	if data, err := os.ReadFile(filepath.Join(s.RootDir, "service.log")); err == nil {
		fmt.Fprintf(os.Stderr, "\n--- 集成测试服务日志 ---\n%s\n", data)
	}
}

// RequestJSON 发起 HTTP 请求并解析框架默认响应外壳。
func (s *Suite) RequestJSON(t testing.TB, method, path, token string, body interface{}) ResponseEnvelope {
	t.Helper()
	envelope, err := s.DoJSON(method, path, token, body)
	require.NoError(t, err)
	return envelope
}

// DoJSON 发起真实 HTTP 请求并返回可由测试或 benchmark 自行处理的结果。
func (s *Suite) DoJSON(method, path, token string, body interface{}) (ResponseEnvelope, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return ResponseEnvelope{}, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, s.BaseURL+path, reader)
	if err != nil {
		return ResponseEnvelope{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return ResponseEnvelope{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return ResponseEnvelope{}, err
	}
	envelope := ResponseEnvelope{HTTPStatus: response.StatusCode, Body: string(data)}
	if err := json.Unmarshal(data, &envelope); err != nil {
		if response.StatusCode == http.StatusOK {
			return envelope, fmt.Errorf("解析成功响应失败: %w", err)
		}
	}
	return envelope, nil
}

// TransportStats reads the request-bound ServiceContext snapshot through the
// local-only server-management route. It never infers protocol use from logs.
func (s *Suite) TransportStats(t testing.TB) TransportStatsSnapshot {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/servermanage/transportstats", "", nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var snapshot TransportStatsSnapshot
	require.NoError(t, json.Unmarshal(envelope.Data, &snapshot))
	return snapshot
}

// TokenFor 通过框架内建 TestToken 路由获取指定用户类型的令牌。
func (s *Suite) TokenFor(t testing.TB, userID string, tokenType int) string {
	t.Helper()
	path := "/api/servermanage/testtoken?userid=" + userID
	if tokenType != 0 {
		path += "&type=" + strconv.Itoa(tokenType)
	}
	envelope := s.RequestJSON(t, http.MethodGet, path, "", nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	token, err := AccessTokenFromData(envelope.Data)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	return token
}

// TokenPoolFor 在 benchmark 计时前创建一组可轮转的身份令牌。
// 长稳流量应在固定用户池中均匀分散，避免单用户列表无界增长污染读写吞吐曲线。
func (s *Suite) TokenPoolFor(t testing.TB, prefix string, count, tokenType int) []string {
	t.Helper()
	if count <= 0 {
		count = 1
	}
	tokens := make([]string, count)
	for index := range tokens {
		tokens[index] = s.TokenFor(t, fmt.Sprintf("%s-%d", prefix, index), tokenType)
	}
	return tokens
}

// WriteWebSocket 发送符合框架协议的 WebSocket 消息。
func (s *Suite) WriteWebSocket(t testing.TB, connection *websocket.Conn, event, channel string, data interface{}) {
	t.Helper()
	require.NoError(t, connection.WriteJSON(map[string]interface{}{
		"event":   event,
		"channel": channel,
		"data":    data,
	}))
}

// ReadWebSocket 在给定时限内读取并解析一条 WebSocket 消息。
func (s *Suite) ReadWebSocket(t testing.TB, connection *websocket.Conn, timeout time.Duration) WebSocketMessage {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(timeout)))
	_, data, err := connection.ReadMessage()
	require.NoError(t, err)
	var message WebSocketMessage
	require.NoError(t, json.Unmarshal(data, &message), string(data))
	return message
}

// StreamWebSocket 持续读取指定连接，使多次“没有消息”断言不会破坏连接状态。
func (s *Suite) StreamWebSocket(t testing.TB, connection *websocket.Conn) <-chan WebSocketMessage {
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
