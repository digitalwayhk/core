// 本文件保留 07 all-in-one 默认端口，并让显式启动参数交由框架处理。
package main

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/run"
)

func setDefaultPorts(server *run.WebServer, args []string) {
	server.Port, server.GRPCPort = 48180, 58180
	for _, arg := range args {
		if arg == "--" {
			break
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		switch name {
		case "p":
			server.Port = 0
		case "grpc":
			server.GRPCPort = 0
		}
	}
}
