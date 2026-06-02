package config

import (
	"errors"
	"time"
)

// ClusterConfig 集群配置。Mode=off 时单机运行，Mode=auto 时自动检测，Mode=on 时强制进入集群流程。
type ClusterConfig struct {
	Mode                  string                        `json:",optional"` // off | auto | on
	Provider              string                        `json:",optional"` // local | etcd | consul
	NodeName              string                        `json:",optional"`
	AdvertiseAddress      string                        `json:",optional"`
	HeartbeatInterval     time.Duration                 `json:",optional"`
	HeartbeatTimeout      time.Duration                 `json:",optional"`
	SuspectTimeout        time.Duration                 `json:",optional"`
	InstanceReuseCooldown time.Duration                 `json:",optional"`
	Claim                 ClusterClaimConfig            `json:",optional"`
	Discovery             ClusterDiscoveryConfig        `json:",optional"`
	Shard                 ClusterShardConfig            `json:",optional"`
	Services              map[string]ClusterServiceConfig `json:",optional"`
	Providers             ClusterProviderConfig         `json:",optional"`
}

// ClusterClaimConfig 实例身份认领配置。
type ClusterClaimConfig struct {
	AutoMachineID    bool   `json:",optional"`
	AutoDataCenterID bool   `json:",optional"`
	MachineIDMax     uint   `json:",optional"`
	DataCenterIDMax  uint   `json:",optional"`
	ConflictPolicy   string `json:",optional"` // expand-machine-id | expand-data-center-id | fail
}

// ClusterDiscoveryConfig 服务发现配置。
type ClusterDiscoveryConfig struct {
	Seeds     []string `json:",optional"`
	Multicast bool     `json:",optional"`
	MDNS      bool     `json:",optional"`
}

// ClusterShardConfig 服务分片全局策略配置。
type ClusterShardConfig struct {
	MissingKeyPolicy     string   `json:",optional"` // error | average
	EmptyCandidatePolicy string   `json:",optional"` // error | average | readonly-fallback
	KeyPriority          []string `json:",optional"`
}

// ClusterServiceConfig 单个服务的分片配置。
type ClusterServiceConfig struct {
	ShardKeys map[string]ClusterShardKeyConfig    `json:",optional"`
	Instances []ClusterInstanceShardConfig        `json:",optional"`
}

// ClusterShardKeyConfig 单个 shard key 的规则。
type ClusterShardKeyConfig struct {
	Mode     string `json:",optional"` // exact | group | hash | optional
	Required bool   `json:",optional"`
}

// ClusterInstanceShardConfig 单个实例的分片标记。
type ClusterInstanceShardConfig struct {
	MachineID uint                `json:",optional"`
	Shards    map[string][]string `json:",optional"`
}

// ClusterProviderConfig 外部 provider 连接参数。
type ClusterProviderConfig struct {
	Etcd   EtcdProviderConfig   `json:",optional"`
	Consul ConsulProviderConfig `json:",optional"`
}

// EtcdProviderConfig etcd 连接配置。
type EtcdProviderConfig struct {
	Endpoints []string      `json:",optional"`
	Prefix    string        `json:",optional"`
	TTL       time.Duration `json:",optional"`
}

// ConsulProviderConfig Consul 连接配置。
type ConsulProviderConfig struct {
	Address string        `json:",optional"`
	Prefix  string        `json:",optional"`
	TTL     time.Duration `json:",optional"`
}

// ApplyDefaults 为 ClusterConfig 补充缺失的默认值。
func (c *ClusterConfig) ApplyDefaults() {
	if c.Mode == "" {
		c.Mode = "auto"
	}
	if c.Provider == "" {
		c.Provider = "local"
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 3 * time.Second
	}
	if c.HeartbeatTimeout == 0 {
		c.HeartbeatTimeout = 10 * time.Second
	}
	if c.SuspectTimeout == 0 {
		c.SuspectTimeout = 15 * time.Second
	}
	if c.InstanceReuseCooldown == 0 {
		c.InstanceReuseCooldown = 30 * time.Second
	}
	if c.Claim.ConflictPolicy == "" {
		c.Claim.ConflictPolicy = "expand-machine-id"
	}
	if c.Claim.MachineIDMax == 0 {
		c.Claim.MachineIDMax = 31
	}
	if c.Claim.DataCenterIDMax == 0 {
		c.Claim.DataCenterIDMax = 31
	}
	if c.Shard.MissingKeyPolicy == "" {
		c.Shard.MissingKeyPolicy = "error"
	}
	if c.Shard.EmptyCandidatePolicy == "" {
		c.Shard.EmptyCandidatePolicy = "error"
	}
	if c.Services == nil {
		c.Services = make(map[string]ClusterServiceConfig)
	}
	if c.Providers.Etcd.Prefix == "" {
		c.Providers.Etcd.Prefix = "/digitalway/core"
	}
	if c.Providers.Etcd.TTL == 0 {
		c.Providers.Etcd.TTL = 10 * time.Second
	}
	if c.Providers.Consul.Prefix == "" {
		c.Providers.Consul.Prefix = "digitalway-core"
	}
	if c.Providers.Consul.TTL == 0 {
		c.Providers.Consul.TTL = 10 * time.Second
	}
}

// Validate 校验 ClusterConfig 中的字段合法性。
func (c *ClusterConfig) Validate() error {
	switch c.Mode {
	case "off", "auto", "on":
	default:
		return errors.New("cluster.mode must be off, auto, or on")
	}
	switch c.Provider {
	case "local", "etcd", "consul":
	default:
		return errors.New("cluster.provider must be local, etcd, or consul")
	}
	if c.Mode == "on" && c.Provider == "etcd" && len(c.Providers.Etcd.Endpoints) == 0 {
		return errors.New("cluster.providers.etcd.endpoints is required when provider=etcd and mode=on")
	}
	if c.Mode == "on" && c.Provider == "consul" && c.Providers.Consul.Address == "" {
		return errors.New("cluster.providers.consul.address is required when provider=consul and mode=on")
	}
	// 空字符串表示"未配置，使用 ApplyDefaults 后的默认值"
	switch c.Claim.ConflictPolicy {
	case "", "expand-machine-id", "expand-data-center-id", "fail":
	default:
		return errors.New("cluster.claim.conflictPolicy must be expand-machine-id, expand-data-center-id, or fail")
	}
	return nil
}
