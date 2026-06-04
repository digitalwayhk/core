package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
)

func main() {
	etcd := config.ClusterConfig{
		Mode:     "on",
		Provider: "etcd",
	}
	etcd.ApplyDefaults()
	etcd.Providers.Etcd.Endpoints = []string{"127.0.0.1:2379"}
	etcd.Providers.Etcd.TTL = 10 * time.Second

	consul := config.ClusterConfig{
		Mode:     "on",
		Provider: "consul",
	}
	consul.ApplyDefaults()
	consul.Providers.Consul.Address = "127.0.0.1:8500"
	consul.Providers.Consul.TTL = 10 * time.Second

	if err := etcd.Validate(); err != nil {
		panic(err)
	}
	if err := consul.Validate(); err != nil {
		panic(err)
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"etcd":   etcd,
		"consul": consul,
	}, "", "  ")
	fmt.Println(string(data))
}
