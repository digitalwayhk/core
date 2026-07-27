package config

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/gofrs/uuid"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

type ServerConfig struct {
	rest.RestConf
	DataCenterID          uint
	MachineID             uint
	Auth                  AuthSecret
	ManageAuth            AuthSecret
	ServerManageAuth      AuthSecret
	IsWhiteList           bool
	WhiteList             []string
	TrustedProxies        []string
	IsLoaclVisit          bool
	RemoteAccessManageAPI bool
	MelodyConfigPath      string               `json:",optional"`
	Cluster               ClusterConfig        `json:",optional"`
	Transport             TransportConfig      `json:",optional"`
	MQ                    MQConfig             `json:",optional"`
	RouteCache             RouteCacheConfig             `json:",optional"`
	AuthRevocation         AuthRevocationConfig         `json:",optional"`
	RuntimeObservability   RuntimeObservabilityConfig   `json:",optional"`
}

// ApplyDefaults 为 ServerConfig 及其子配置补充缺失的默认值。
// ReadConfig、NewServiceDefaultConfig、Save 均必须调用此方法。
func (con *ServerConfig) ApplyDefaults() {
	if con.WhiteList == nil {
		con.WhiteList = make([]string, 0)
	}
	if con.TrustedProxies == nil {
		con.TrustedProxies = make([]string, 0)
	}
	con.Cluster.ApplyDefaults()
	con.Transport.ApplyDefaults()
	con.Transport.ApplyServerDefaults(con.Cluster, con.Port)
	con.MQ.ApplyDefaults()
	con.RouteCache.ApplyDefaults()
	con.AuthRevocation.ApplyDefaults(con.Name)
	con.RuntimeObservability.ApplyDefaults()
}

// Validate 校验 ServerConfig 中各子配置的合法性。
func (con *ServerConfig) Validate() error {
	for i, proxy := range con.TrustedProxies {
		proxy = strings.TrimSpace(proxy)
		if _, err := netip.ParseAddr(proxy); err == nil {
			continue
		}
		if _, err := netip.ParsePrefix(proxy); err != nil {
			return fmt.Errorf("TrustedProxies[%d] must be an IP address or CIDR: %q", i, proxy)
		}
	}
	if err := con.Cluster.Validate(); err != nil {
		return err
	}
	if err := con.Transport.Validate(); err != nil {
		return err
	}
	if err := con.Transport.ValidateForServer(con.Cluster, utils.GetLocalIP()); err != nil {
		return err
	}
	if err := con.MQ.Validate(); err != nil {
		return err
	}
	if err := con.RouteCache.Validate(); err != nil {
		return err
	}
	casdoorEnabled := con.Auth.CasDoor.Enable || con.ManageAuth.CasDoor.Enable
	if err := con.AuthRevocation.Validate(casdoorEnabled); err != nil {
		return err
	}
	if err := con.RuntimeObservability.Validate(); err != nil {
		return err
	}
	if err := con.validateCasdoorSecrets(); err != nil {
		return err
	}
	return nil
}

func (con *ServerConfig) validateCasdoorSecrets() error {
	authWebhook := strings.TrimSpace(con.Auth.CasDoor.WebhookSecret)
	manageWebhook := strings.TrimSpace(con.ManageAuth.CasDoor.WebhookSecret)
	if strings.TrimSpace(con.ServerManageAuth.CasDoor.WebhookSecret) != "" {
		return fmt.Errorf("ServerManageAuth.CasDoor.WebhookSecret is unsupported")
	}
	if con.Auth.CasDoor.Enable && authWebhook == "" {
		return fmt.Errorf("Auth.CasDoor.WebhookSecret is required")
	}
	if con.ManageAuth.CasDoor.Enable && manageWebhook == "" {
		return fmt.Errorf("ManageAuth.CasDoor.WebhookSecret is required")
	}
	if authWebhook != "" && manageWebhook != "" && subtleSecretEqual(authWebhook, manageWebhook) {
		return fmt.Errorf("Auth and ManageAuth CasDoor WebhookSecret must be different")
	}
	protected := []struct {
		name  string
		value string
	}{
		{"Auth.AccessSecret", con.Auth.AccessSecret},
		{"Auth.RefreshSecret", con.Auth.RefreshSecret},
		{"ManageAuth.AccessSecret", con.ManageAuth.AccessSecret},
		{"ManageAuth.RefreshSecret", con.ManageAuth.RefreshSecret},
		{"ServerManageAuth.AccessSecret", con.ServerManageAuth.AccessSecret},
	}
	for _, webhook := range []struct {
		name  string
		value string
	}{{"Auth.CasDoor.WebhookSecret", authWebhook}, {"ManageAuth.CasDoor.WebhookSecret", manageWebhook}} {
		if webhook.value == "" {
			continue
		}
		for _, secret := range protected {
			if secret.value != "" && subtleSecretEqual(webhook.value, secret.value) {
				return fmt.Errorf("%s must be different from %s", webhook.name, secret.name)
			}
		}
	}
	return nil
}

func subtleSecretEqual(left, right string) bool {
	if len(left) != len(right) || left == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// ReloadExternalConfigs 加载外部配置文件（Casdoor、Melody）。
func (con *ServerConfig) ReloadExternalConfigs() {
	if con.Auth.CasDoor.Enable {
		if err := con.Auth.CasDoor.ReloadConfig(); err != nil {
			panic(err)
		}
	}
	if con.ManageAuth.CasDoor.Enable {
		if err := con.ManageAuth.CasDoor.ReloadConfig(); err != nil {
			panic(err)
		}
	}
	if con.ServerManageAuth.CasDoor.Enable {
		if err := con.ServerManageAuth.CasDoor.ReloadConfig(); err != nil {
			panic(err)
		}
	}
	if con.MelodyConfigPath != "" {
		if err := loadMelodyConfig(con.MelodyConfigPath); err != nil {
			panic(err)
		}
	}
}

type AuthSecret struct {
	AccessSecret  string
	AccessExpire  int64
	RefreshSecret string
	RefreshExpire int64
	CasDoor       CasDoorConfig
}

const (
	DefaultAccessExpireSeconds  int64 = 7200
	DefaultRefreshExpireSeconds int64 = 2592000
)

const CONFIGDIR = "/etc/"

var CONFIGDIRPATH = utils.Getpath() + CONFIGDIR

var (
	serverInitializationMu   sync.RWMutex
	activeServerInitializers int

	// INITSERVER 仅为保持源码兼容而保留。
	// Deprecated: 请使用 IsServerInitializing；外部并发直接写入 INITSERVER 不受锁保护。
	INITSERVER = false
)

// BeginServerInitialization 登记一个进入初始化阶段的服务器实例。
func BeginServerInitialization() {
	serverInitializationMu.Lock()
	activeServerInitializers++
	INITSERVER = true
	serverInitializationMu.Unlock()
}

// EndServerInitialization 结束一个服务器实例的初始化阶段。
func EndServerInitialization() {
	serverInitializationMu.Lock()
	if activeServerInitializers > 0 {
		activeServerInitializers--
	}
	INITSERVER = activeServerInitializers > 0
	serverInitializationMu.Unlock()
}

// IsServerInitializing 返回当前进程是否仍有服务器实例处于初始化阶段。
func IsServerInitializing() bool {
	serverInitializationMu.RLock()
	defer serverInitializationMu.RUnlock()
	return INITSERVER
}

func NewServiceDefaultConfig(servicename string, port int) *ServerConfig {
	var con ServerConfig
	con.Name = servicename
	str := "{\"Name\":\"" + servicename + "\",\"Port\":" + strconv.Itoa(port) + ",\"Host\":\"0.0.0.0\"}"
	conf.LoadConfigFromJsonBytes([]byte(str), &con)
	con.Telemetry.Batcher = "zipkin"
	ip := utils.GetLocalIP()
	con.Log.ServiceName = servicename + "-" + ip
	con.Log.KeepDays = 10
	con.Log.Level = "info"
	//con.Log.Mode = "file"
	//con.Log.Path = "logs/" + servicename
	con.Auth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.Auth.AccessExpire = DefaultAccessExpireSeconds
	con.Auth.RefreshSecret = uuid.Must(uuid.NewV4()).String()
	con.Auth.RefreshExpire = DefaultRefreshExpireSeconds
	con.Auth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.ManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ManageAuth.AccessExpire = DefaultAccessExpireSeconds
	con.ManageAuth.RefreshSecret = uuid.Must(uuid.NewV4()).String()
	con.ManageAuth.RefreshExpire = DefaultRefreshExpireSeconds
	con.ManageAuth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.ServerManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ServerManageAuth.AccessExpire = 86400
	con.ServerManageAuth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.IsWhiteList = false
	con.WhiteList = make([]string, 0)
	con.TrustedProxies = make([]string, 0)
	con.MelodyConfigPath = ""
	con.ApplyDefaults()
	if err := con.Validate(); err != nil {
		panic(err)
	}
	return &con
}
func ReadConfig(servicename string) *ServerConfig {
	file := CONFIGDIRPATH + servicename + ".json"
	if !utils.IsExista(file) {
		return nil
	}

	// Auto-migrate old config files whose time.Duration fields were serialized
	// as int64 nanoseconds (e.g. 3000000000) instead of strings (e.g. "3s").
	if err := migrateConfig(file); err != nil {
		logx.Errorw("config_migration_failed",
			logx.Field("config_path", file),
			logx.Field("error", err),
		)
	}

	con := &ServerConfig{}
	conf.MustLoad(file, con)
	con.ApplyDefaults()
	if err := con.Validate(); err != nil {
		panic(err)
	}
	con.ReloadExternalConfigs()
	return con
}

// migrateConfig rewrites the config file in-place to fix known format issues
// from older core versions: numeric time.Duration fields, null slices that
// should be empty arrays, etc. Migration errors are logged but non-fatal.
func migrateConfig(file string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read config migration source: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("decode config migration source: %w", err)
	}

	changed := migrateDurations(m)
	if migrateNullSlices(m) {
		changed = true
	}
	if migrateRefreshSecrets(m) {
		changed = true
	}
	if migrateRemovedSocketConfig(m) {
		changed = true
	}
	if migrateRetiredTopLevelConfig(m) {
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode migrated config: %w", err)
	}
	if err := writeConfigFile(file, out); err != nil {
		return fmt.Errorf("write migrated config: %w", err)
	}
	return nil
}

// migrateRetiredTopLevelConfig 删除不再参与运行时行为的历史配置，同时保留未知字段。
func migrateRetiredTopLevelConfig(m map[string]interface{}) bool {
	changed := false
	for _, key := range []string{
		"RunIp",
		"ParentServerIP",
		"AttachServices",
		"Debug",
		"CustomerDataList",
	} {
		if _, ok := m[key]; ok {
			delete(m, key)
			changed = true
		}
	}
	for _, key := range []string{"Auth", "ManageAuth", "ServerManageAuth"} {
		auth, ok := m[key].(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := auth["Logto"]; ok {
			delete(auth, "Logto")
			changed = true
		}
	}
	return changed
}

// migrateRemovedSocketConfig removes retired transport selectors while leaving
// unrelated and unknown user configuration untouched.
func migrateRemovedSocketConfig(m map[string]interface{}) bool {
	changed := false
	if _, ok := m["SocketPort"]; ok {
		delete(m, "SocketPort")
		changed = true
	}
	if transportConfig, ok := m["Transport"].(map[string]interface{}); ok {
		if _, ok := transportConfig["Socket"]; ok {
			delete(transportConfig, "Socket")
			changed = true
		}
		if grpcConfig, ok := transportConfig["GRPC"].(map[string]interface{}); ok {
			if _, ok := grpcConfig["Enable"]; ok {
				delete(grpcConfig, "Enable")
				changed = true
			}
		}
	}
	return changed
}

// migrateRefreshSecrets 为已有 AccessSecret 的历史 auth/manage 配置生成一次性
// Refresh 密钥。密钥会由 migrateConfig 回写，后续启动不得重新生成。
func migrateRefreshSecrets(m map[string]interface{}) bool {
	changed := false
	for _, key := range []string{"Auth", "ManageAuth"} {
		auth, ok := m[key].(map[string]interface{})
		if !ok {
			continue
		}
		accessSecret, _ := auth["AccessSecret"].(string)
		if strings.TrimSpace(accessSecret) == "" {
			continue
		}
		refreshSecret, _ := auth["RefreshSecret"].(string)
		if strings.TrimSpace(refreshSecret) == "" {
			for refreshSecret == "" || refreshSecret == accessSecret {
				refreshSecret = uuid.Must(uuid.NewV4()).String()
			}
			auth["RefreshSecret"] = refreshSecret
			changed = true
		}
		refreshExpire, ok := auth["RefreshExpire"].(float64)
		if !ok || refreshExpire <= 0 {
			auth["RefreshExpire"] = DefaultRefreshExpireSeconds
			changed = true
		}
	}
	return changed
}

func writeConfigFile(file string, data []byte) error {
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(file, 0o600)
}

// migrateNullSlices converts nil JSON values to empty arrays for fields
// whose Go types are slices that must not be nil (e.g. []string).
func migrateNullSlices(m map[string]interface{}) bool {
	changed := false
	for _, key := range []string{"PrivateKeys", "Endpoints", "Brokers", "NameServers", "TrustedProxies"} {
		if v, ok := m[key]; ok && v == nil {
			m[key] = []interface{}{}
			changed = true
		}
	}
	// Recurse into nested objects.
	for _, v := range m {
		if nested, ok := v.(map[string]interface{}); ok {
			if migrateNullSlices(nested) {
				changed = true
			}
		}
	}
	return changed
}

// migrateDurations walks a JSON map and converts numeric values at duration
// keys to their string forms. Returns true if any conversion was done.
func migrateDurations(m map[string]interface{}) bool {
	changed := false
	for k, v := range m {
		switch v := v.(type) {
		case float64:
			if isDurationKey(k) {
				dur := time.Duration(int64(v))
				m[k] = dur.String()
				changed = true
			}
		case map[string]interface{}:
			if migrateDurations(v) {
				changed = true
			}
		}
	}
	return changed
}

// isDurationKey returns true for JSON keys that are known time.Duration fields.
// Keep in sync with all time.Duration fields across config structs.
func isDurationKey(k string) bool {
	switch k {
	case
		"UploadRate", "CheckInterval", "ProfilingDuration",
		"WrapUpTime", "WaitTime",
		"HeartbeatInterval", "HeartbeatTimeout", "SuspectTimeout",
		"InstanceReuseCooldown", "TTL",
		"RetryDelay", "InitialDelay", "MaxDelay",
		"DualWriteDuration",
		"QueryTimeout", "CacheTTL":
		return true
	}
	return false
}
func (own *ServerConfig) Save() error {
	if utils.IsTest() {
		return nil
	}
	own.ApplyDefaults()
	if err := own.Validate(); err != nil {
		return err
	}
	file := CONFIGDIRPATH + own.Name + ".json"
	if !utils.IsExista(file) {
		_, err := utils.CreateDir("etc")
		if err != nil {
			panic(err)
		}
	}

	// Marshal to a map so we can fix time.Duration fields before writing.
	data, err := json.Marshal(own)
	if err != nil {
		return err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	// Walk the config struct with reflection to find all time.Duration fields
	// and convert them from int64 nanoseconds to their string form (e.g. "3s").
	fixDurations(reflect.ValueOf(own).Elem(), m)

	// Handle fields that need special treatment.
	if own.Signature.PrivateKeys == nil {
		if sig, ok := m["Signature"].(map[string]interface{}); ok {
			sig["PrivateKeys"] = []interface{}{}
		}
	}
	// Signature.Expiry is serialized as nanoseconds; convert to hours string.
	if expH := int(own.Signature.Expiry / time.Hour); expH > 0 {
		if sig, ok := m["Signature"].(map[string]interface{}); ok {
			sig["Expiry"] = strconv.Itoa(expH) + "h"
		}
	}

	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeConfigFile(file, utils.String2Bytes(string(out)))
}

// fixDurations walks v (a struct value) and replaces every time.Duration
// field's entry in m with the duration's .String() form. Embedded structs
// are flattened; named struct fields recurse with their json tag as key.
func fixDurations(v reflect.Value, m map[string]interface{}) {
	t := v.Type()
	n := t.NumField()
	for i := 0; i < n; i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fv := v.Field(i)
		ft := f.Type

		// Determine the JSON key for this field.
		jsonKey := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			if name := strings.Split(tag, ",")[0]; name != "" {
				jsonKey = name
			}
		}

		if ft == reflect.TypeOf(time.Duration(0)) {
			// time.Duration → convert nanoseconds → string.
			if fv.IsValid() {
				dur := time.Duration(fv.Int())
				if entry, ok := m[jsonKey]; ok {
					// Only replace if the entry is a number (don't touch strings).
					if _, isNum := entry.(float64); isNum {
						m[jsonKey] = dur.String()
					}
				}
			}
			continue
		}

		// Handle pointer types — dereference if non-nil.
		if ft.Kind() == reflect.Ptr && !fv.IsNil() {
			fv = fv.Elem()
			ft = fv.Type()
		}

		if ft.Kind() == reflect.Struct && ft != reflect.TypeOf(time.Time{}) {
			if f.Anonymous {
				// Embedded struct — flatten its fields into the same map.
				fixDurations(fv, m)
			} else if nested, ok := m[jsonKey].(map[string]interface{}); ok {
				fixDurations(fv, nested)
			}
		}
	}
}
