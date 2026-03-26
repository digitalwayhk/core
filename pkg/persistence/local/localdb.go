package local

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

func GetDbPath(path string) (string, error) {
	path = strings.TrimPrefix(path, ".")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if !strings.HasPrefix(path, "db") {
		if !strings.Contains(path, "/") {
			path = filepath.Join("db", path, path)
		} else {
			path = filepath.Join("db", path)
		}
	}

	basePath := utils.Getpath()
	if basePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", errors.New("get working dir error:" + err.Error())
		}
		basePath = cwd
	}

	dns := filepath.Join(basePath, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(dns), 0o777); err != nil {
		return "", errors.New("create db dir error:" + err.Error())
	}
	return dns, nil
}
func GetDbFileInfo(file string) (time.Time, int64) {
	fi, err := os.Stat(file)
	if err != nil {
		logx.Error("get db file info error:", file, err)
	}
	return fi.ModTime(), fi.Size()
}
