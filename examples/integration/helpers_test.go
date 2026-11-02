package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReservePortRangeUsesDedicatedLowRange(t *testing.T) {
	const (
		count    = 4
		rangeMin = 20000
		rangeMax = 44999
	)
	base, err := reservePortRange(count)
	require.NoError(t, err)
	require.GreaterOrEqual(t, base, rangeMin)
	require.LessOrEqual(t, base+count-1, rangeMax)

	listeners := make([]net.Listener, 0, count)
	t.Cleanup(func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	})
	for offset := 0; offset < count; offset++ {
		listener, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+offset)))
		require.NoError(t, listenErr)
		listeners = append(listeners, listener)
	}
}

func TestAccessTokenFromData(t *testing.T) {
	t.Run("解析结构化响应", func(t *testing.T) {
		data, err := json.Marshal(TokenResponse{AccessToken: "access-token"})
		require.NoError(t, err)

		token, err := AccessTokenFromData(data)
		require.NoError(t, err)
		require.Equal(t, "access-token", token)
	})

	t.Run("兼容旧字符串响应", func(t *testing.T) {
		data, err := json.Marshal("legacy-token")
		require.NoError(t, err)

		token, err := AccessTokenFromData(data)
		require.NoError(t, err)
		require.Equal(t, "legacy-token", token)
	})

	t.Run("拒绝缺少访问令牌", func(t *testing.T) {
		token, err := AccessTokenFromData(json.RawMessage(`{"refresh_token":"refresh-token"}`))
		require.Error(t, err)
		require.Empty(t, token)
	})
}

func TestStopProcessReportsActualExitStatus(t *testing.T) {
	for _, code := range []int{0, 66} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			command := exec.Command(executable, "-test.run=^TestStopProcessChild$")
			command.Env = append(os.Environ(), fmt.Sprintf("CORE_STOP_PROCESS_CHILD=%d", code))
			output, err := command.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, command.Start())
			defer command.Process.Kill()
			scanner := bufio.NewScanner(output)
			require.True(t, scanner.Scan())
			require.Equal(t, "READY", scanner.Text())
			suite := &Suite{command: command}
			err = suite.StopProcess()
			if code == 0 {
				require.NoError(t, err)
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, code, exit.ExitCode())
			}
			require.NoError(t, suite.StopProcess(), "已退出后的重复关闭保持幂等")
		})
	}
}

// 本子进程夹具只在上面测试显式设置环境变量后接收真实退出信号。
func TestStopProcessChild(t *testing.T) {
	raw := os.Getenv("CORE_STOP_PROCESS_CHILD")
	if raw == "" {
		return
	}
	code, err := strconv.Atoi(raw)
	if err != nil {
		os.Exit(2)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	fmt.Println("READY")
	<-stop
	os.Exit(code)
}
