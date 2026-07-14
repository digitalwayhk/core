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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Suite 保存一次集成测试进程共享的服务地址和子进程资源。
type Suite struct {
	RootDir      string
	BasePort     int
	BaseURL      string
	WebSocketURL string
	Binary       string
	Arguments    []string
	command      *exec.Cmd
	outputFile   *os.File
}

// ProcessOptions 描述一个与具体业务无关的示例进程启动方式。
type ProcessOptions struct {
	BuildPackage string
	BinaryName   string
	TempPrefix   string
	ServiceCount int
	ServiceIndex int
	Arguments    []string
	DisableRace  bool
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

	basePort, err := reservePortRange(options.ServiceCount)
	if err != nil {
		cleanup()
		return nil, err
	}
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

	arguments := append([]string{}, options.Arguments...)
	arguments = append(arguments, "-p", strconv.Itoa(basePort))
	suite := &Suite{
		RootDir: rootDir, BasePort: basePort,
		BaseURL:      "http://127.0.0.1:" + strconv.Itoa(basePort+options.ServiceIndex),
		WebSocketURL: "ws://127.0.0.1:" + strconv.Itoa(basePort+options.ServiceIndex) + "/ws",
		Binary:       binary, Arguments: arguments,
	}
	if err := suite.Restart(); err != nil {
		cleanup()
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
	response, err := http.DefaultClient.Do(request)
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

// TokenFor 通过框架内建 TestToken 路由获取指定用户类型的令牌。
func (s *Suite) TokenFor(t testing.TB, userID string, tokenType int) string {
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
