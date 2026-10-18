// 本程序作为独立进程发送07订单负载，仅依赖标准库和orderload，不加载框架初始化。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/digitalwayhk/core/examples/internal/orderload"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "权限0600的阶段配置文件")
	flag.Parse()
	info, err := os.Stat(*configPath)
	if err != nil {
		return errors.New("无法读取负载配置文件")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return errors.New("负载配置文件必须为权限受限的普通文件，大小不超过1MiB")
	}
	file, err := os.Open(*configPath)
	if err != nil {
		return errors.New("无法打开负载配置文件")
	}
	var stage orderload.Stage
	err = json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&stage)
	closeErr := file.Close()
	if err != nil {
		return errors.New("负载配置JSON无效")
	}
	if closeErr != nil {
		return closeErr
	}
	if err := stage.Validate(); err != nil {
		return err
	}
	if err := os.Mkdir(stage.Output, 0700); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConns: 512, MaxIdleConnsPerHost: 512, MaxConnsPerHost: 512}}
	defer client.CloseIdleConnections()
	results, summary := orderload.RunStage(ctx, client, stage.URL, stage.Token, stage.ProductID, stage.Rate, time.Duration(stage.DurationMS)*time.Millisecond, stage.Origin, stage.Namespace)
	for name, value := range map[string]any{"requests.json": results, "summary.json": summary} {
		file, err := os.OpenFile(filepath.Join(stage.Output, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		err = json.NewEncoder(file).Encode(value)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
