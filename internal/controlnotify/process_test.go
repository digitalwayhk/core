// 本文件使用独立进程及可显式启用的测试 Broker 重启验证通知扇出与连接失效。
package controlnotify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
)

func TestInternalNotificationProcessHelper(t *testing.T) {
	if os.Getenv("CORE_NOTIFICATION_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	var cfg config.MQConfig
	if err := json.Unmarshal([]byte(os.Getenv("CORE_NOTIFICATION_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	x, err := Open(ctx, cfg, "multiprocess", "cache")
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	fmt.Println("READY")
	for i := 0; i < 32; i++ {
		data, err := x.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "broadcast" {
			t.Fatal("unexpected frame")
		}
	}
	fmt.Println("RECEIVED")
}

func TestInternalNotificationMultiProcessFanout(t *testing.T) {
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		encoded, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		var commands []*exec.Cmd
		defer func() {
			cancel()
			for _, command := range commands {
				if command.ProcessState == nil {
					_ = command.Wait()
				}
			}
		}()
		var scanners []*bufio.Scanner
		for i := 0; i < 2; i++ {
			command := exec.CommandContext(ctx, executable, "-test.run=^TestInternalNotificationProcessHelper$")
			command.Env = append(os.Environ(), "CORE_NOTIFICATION_HELPER=1", "CORE_NOTIFICATION_CONFIG="+string(encoded))
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			command.Stderr = command.Stdout
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, command)
			scanner := bufio.NewScanner(output)
			if !scanner.Scan() || scanner.Text() != "READY" {
				t.Fatal("child subscription did not become ready")
			}
			scanners = append(scanners, scanner)
		}
		publisher := testOpen(t, cfg, "multiprocess", "cache")
		for i := 0; i < 32; i++ {
			if err := publisher.Publish(ctx, []byte("broadcast")); err != nil {
				t.Fatal(err)
			}
			if _, err := publisher.Receive(ctx); err != nil {
				t.Fatal(err)
			}
		}
		for i, command := range commands {
			if !scanners[i].Scan() || scanners[i].Text() != "RECEIVED" {
				t.Fatal("independent process missed broadcast")
			}
			for scanners[i].Scan() {
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestInternalNotificationBrokerRestart(t *testing.T) {
	if os.Getenv("CORE_TEST_RESTART_NOTIFY_BROKERS") != "1" {
		t.Skip("NOT RUN: dedicated Broker restart not enabled")
	}
	brokerConfigs(t, func(t *testing.T, cfg config.MQConfig) {
		// 只允许本任务明确创建的隔离容器；绝不接受外部容器名参数。
		container := "core-internal-notify-redis"
		if cfg.Provider == "nats-jetstream" {
			container = "core-internal-notify-nats"
		}
		x := testOpen(t, cfg, "restart", "identity")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := x.Receive(ctx); done <- err }()
		if output, err := exec.CommandContext(ctx, "docker", "restart", container).CombinedOutput(); err != nil {
			t.Fatalf("restart fixture: %v %s", err, output)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("old receiver hid restart gap")
			}
		case <-ctx.Done():
			t.Fatal("old receiver did not detect restart")
		}
		var replacement Transport
		for {
			var err error
			replacement, err = Open(ctx, cfg, "restart", "identity")
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(err)
			case <-time.After(50 * time.Millisecond):
			}
		}
		defer replacement.Close()
		if err := replacement.Publish(ctx, []byte("recovered")); err != nil {
			t.Fatal(err)
		}
		if _, err := replacement.Receive(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
