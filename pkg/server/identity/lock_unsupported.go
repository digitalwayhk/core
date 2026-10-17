//go:build !linux && !darwin

// 本文件对尚未验收的持久卷锁平台明确拒绝稳定模式。
package identity

import (
	"errors"
	"os"
)

func lockFile(string) (*os.File, error) {
	return nil, errors.New("identity: stable file locking requires Linux or macOS")
}
func unlockFile(*os.File) error { return nil }
