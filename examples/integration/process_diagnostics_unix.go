//go:build linux || darwin || freebsd

package integration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DumpGoroutines 在失败窗口中捕获当前夹具进程的go-zero SIGUSR1栈，不启用持续profile。
// 调用方必须在停止进程前等待本方法结束；只读取本PID新生成的诊断文件。
func (s *Suite) DumpGoroutines(destination string) error {
	if s == nil || s.command == nil || s.command.Process == nil {
		return fmt.Errorf("诊断进程未启动")
	}
	if !filepath.IsAbs(destination) {
		return fmt.Errorf("诊断输出必须为绝对路径")
	}
	directory := s.Environment["TMPDIR"]
	if directory == "" {
		directory = os.TempDir()
	}
	prefix := fmt.Sprintf("%s-%d-goroutines-", filepath.Base(s.Binary), s.command.Process.Pid)
	started := time.Now()
	if err := s.command.Process.Signal(syscall.SIGUSR1); err != nil {
		return err
	}
	for time.Now().Before(started.Add(3 * time.Second)) {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".dump") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(started.Add(-time.Second)) || info.Size() == 0 {
				continue
			}
			if info.Size() > 32<<20 {
				return fmt.Errorf("诊断栈超过32MiB，保留原文件")
			}
			// 等待信号处理器完成写出；随后检查大小稳定，避免复制正在增长的文件。
			time.Sleep(50 * time.Millisecond)
			source := filepath.Join(directory, entry.Name())
			after, err := os.Stat(source)
			if err != nil || after.Size() != info.Size() {
				continue
			}
			in, err := os.Open(source)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(in, (32<<20)+1))
			_ = in.Close()
			if err != nil {
				return err
			}
			if len(data) > 32<<20 {
				return fmt.Errorf("诊断栈超过32MiB")
			}
			if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return err
			}
			out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, writeErr := out.Write(data)
			closeErr := out.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("等待当前进程的SIGUSR1诊断文件超时")
}
