package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	DefaultClusterHeartbeatInterval     = 3 * time.Second
	DefaultClusterHeartbeatTimeout      = 10 * time.Second
	DefaultClusterSuspectTimeout        = 15 * time.Second
	DefaultClusterInstanceReuseCooldown = 30 * time.Second
	DefaultClusterProviderTTL           = 10 * time.Second
	DefaultClusterEtcdPrefix            = "/digitalway/core"
	DefaultClusterConsulPrefix          = "digitalway-core"
)

// ClusterConfig 集群配置。Mode=off 时单机运行，Mode=auto 时自动检测，Mode=on 时强制进入集群流程。
type ClusterConfig struct {
	Mode                  string                          `json:",optional"` // off | auto | on
	Provider              string                          `json:",optional"` // local | etcd | consul
	NodeName              string                          `json:",optional"`
	AdvertiseAddress      string                          `json:",optional"`
	HeartbeatInterval     time.Duration                   `json:",optional"`
	HeartbeatTimeout      time.Duration                   `json:",optional"`
	SuspectTimeout        time.Duration                   `json:",optional"`
	InstanceReuseCooldown time.Duration                   `json:",optional"`
	Claim                 ClusterClaimConfig              `json:",optional"`
	Discovery             ClusterDiscoveryConfig          `json:",optional"`
	Shard                 ClusterShardConfig              `json:",optional"`
	Services              map[string]ClusterServiceConfig `json:",optional"`
	Providers             ClusterProviderConfig           `json:",optional"`
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
	ShardKeys map[string]ClusterShardKeyConfig `json:",optional"`
	Instances []ClusterInstanceShardConfig     `json:",optional"`
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
		c.HeartbeatInterval = DefaultClusterHeartbeatInterval
	}
	if c.HeartbeatTimeout == 0 {
		c.HeartbeatTimeout = DefaultClusterHeartbeatTimeout
	}
	if c.SuspectTimeout == 0 {
		c.SuspectTimeout = DefaultClusterSuspectTimeout
	}
	if c.InstanceReuseCooldown == 0 {
		c.InstanceReuseCooldown = DefaultClusterInstanceReuseCooldown
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
		c.Providers.Etcd.Prefix = DefaultClusterEtcdPrefix
	}
	if c.Providers.Etcd.TTL == 0 {
		c.Providers.Etcd.TTL = DefaultClusterProviderTTL
	}
	if c.Providers.Consul.Prefix == "" {
		c.Providers.Consul.Prefix = DefaultClusterConsulPrefix
	}
	if c.Providers.Consul.TTL == 0 {
		c.Providers.Consul.TTL = DefaultClusterProviderTTL
	}
}

// Validate 校验 ClusterConfig 中的字段合法性。
func (c *ClusterConfig) Validate() error {
	switch c.Mode {
	case "off", "auto", "on":
	default:
		return errors.New("cluster.mode must be off, auto, or on")
	}
	if c.Mode == "off" {
		return nil
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

	if c.NodeName != "" {
		return errors.New("cluster.nodeName is not implemented; remove this field")
	}
	if c.AdvertiseAddress != "" {
		return errors.New("cluster.advertiseAddress is not implemented; remove this field")
	}
	if err := requireZeroOrDefaultDuration("cluster.heartbeatTimeout", c.HeartbeatTimeout, DefaultClusterHeartbeatTimeout); err != nil {
		return err
	}
	if err := requireZeroOrDefaultDuration("cluster.suspectTimeout", c.SuspectTimeout, DefaultClusterSuspectTimeout); err != nil {
		return err
	}
	if err := requireZeroOrDefaultDuration("cluster.instanceReuseCooldown", c.InstanceReuseCooldown, DefaultClusterInstanceReuseCooldown); err != nil {
		return err
	}

	if c.Claim.AutoMachineID {
		return errors.New("cluster.claim.autoMachineID is not implemented; set it to false")
	}
	if c.Claim.AutoDataCenterID {
		return errors.New("cluster.claim.autoDataCenterID is not implemented; set it to false")
	}
	switch c.Claim.ConflictPolicy {
	case "", "expand-machine-id":
	case "expand-data-center-id", "fail":
		return fmt.Errorf("cluster.claim.conflictPolicy=%q is not implemented; use expand-machine-id", c.Claim.ConflictPolicy)
	default:
		return fmt.Errorf("cluster.claim.conflictPolicy=%q is not implemented; use expand-machine-id", c.Claim.ConflictPolicy)
	}
	if c.Claim.DataCenterIDMax != 0 && c.Claim.DataCenterIDMax != 31 {
		return fmt.Errorf("cluster.claim.dataCenterIDMax is not configurable; use 0 or 31, got %d", c.Claim.DataCenterIDMax)
	}

	if len(c.Discovery.Seeds) != 0 {
		return errors.New("cluster.discovery.seeds is not implemented; remove this field")
	}
	if c.Discovery.Multicast {
		return errors.New("cluster.discovery.multicast is not implemented; set it to false")
	}
	if c.Discovery.MDNS {
		return errors.New("cluster.discovery.mdns is not implemented; set it to false")
	}
	if len(c.Shard.KeyPriority) != 0 {
		return errors.New("cluster.shard.keyPriority is not implemented; remove this field")
	}
	if c.Shard.MissingKeyPolicy != "" && c.Shard.MissingKeyPolicy != "error" {
		return errors.New("cluster.shard.missingKeyPolicy is not implemented for values other than error; use error")
	}
	if c.Shard.EmptyCandidatePolicy != "" && c.Shard.EmptyCandidatePolicy != "error" {
		return errors.New("cluster.shard.emptyCandidatePolicy is not implemented for values other than error; use error")
	}
	if len(c.Services) != 0 {
		return errors.New("cluster.services is not implemented; remove all service entries")
	}

	if c.Providers.Etcd.Prefix != "" && c.Providers.Etcd.Prefix != DefaultClusterEtcdPrefix {
		return fmt.Errorf("cluster.providers.etcd.prefix is not configurable; use %q", DefaultClusterEtcdPrefix)
	}
	if c.Providers.Consul.Prefix != "" && c.Providers.Consul.Prefix != DefaultClusterConsulPrefix {
		return fmt.Errorf("cluster.providers.consul.prefix is not configurable; use %q", DefaultClusterConsulPrefix)
	}
	if err := requireZeroOrDefaultDuration("cluster.providers.consul.ttl", c.Providers.Consul.TTL, DefaultClusterProviderTTL); err != nil {
		return err
	}
	return nil
}

func requireZeroOrDefaultDuration(fieldPath string, value, defaultValue time.Duration) error {
	if value != 0 && value != defaultValue {
		return fmt.Errorf("%s is not configurable; use 0 or %s, got %s", fieldPath, defaultValue, value)
	}
	return nil
}
