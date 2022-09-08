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
	if strings.HasPrefix(path, ".") {
		path = path[1:]
	}
	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}
	if !strings.HasPrefix(path, "db") {
		if strings.Index(path, "/") == -1 {
			path = "db/" + path + "/" + path
		} else {
			path = "db/" + path
		}
	}
	dirs := strings.Split(path, "/")
	if len(dirs) > 1 {
		for i := 0; i < len(dirs)-1; i++ {
			dir := filepath.Join(dirs[:i+1]...)
			//dir = utils.Getpath() + "/" + dir
			if !utils.IsDir(dir) {
				_, err := utils.CreateDir(dir)
				if err != nil {
					return "", errors.New("create db dir error:" + err.Error())
				}
			}
		}
	}
	dns := utils.Getpath() + "/" + path
	return dns, nil
}
func GetDbFileInfo(file string) (time.Time, int64) {
	fi, err := os.Stat(file)
	if err != nil {
		logx.Error("get db file info error:", file, err)
	}
	return fi.ModTime(), fi.Size()
}
