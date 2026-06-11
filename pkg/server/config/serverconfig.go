package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/gofrs/uuid"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

type ServerConfig struct {
	rest.RestConf
	DataCenterID          uint
	MachineID             uint
	Auth                  AuthSecret
	ManageAuth            AuthSecret
	ServerManageAuth      AuthSecret
	RunIp                 string
	ParentServerIP        string
	SocketPort            int
	AttachServices        map[string]*AttachAddress
	Debug                 bool
	IsWhiteList           bool
	WhiteList             []string
	CustomerDataList      []*CustomerData
	IsLoaclVisit          bool
	RemoteAccessManageAPI bool
	MelodyConfigPath      string          `json:",optional"`
	Cluster               ClusterConfig   `json:",optional"`
	Transport             TransportConfig `json:",optional"`
	MQ                    MQConfig        `json:",optional"`
}

// ApplyDefaults 为 ServerConfig 及其子配置补充缺失的默认值。
// ReadConfig、NewServiceDefaultConfig、Save 均必须调用此方法。
func (con *ServerConfig) ApplyDefaults() {
	if con.AttachServices == nil {
		con.AttachServices = make(map[string]*AttachAddress)
	}
	if con.CustomerDataList == nil {
		con.CustomerDataList = make([]*CustomerData, 0)
	}
	if con.WhiteList == nil {
		con.WhiteList = make([]string, 0)
	}
	con.Cluster.ApplyDefaults()
	con.Transport.ApplyDefaults()
	con.MQ.ApplyDefaults()
}

// Validate 校验 ServerConfig 中各子配置的合法性。
func (con *ServerConfig) Validate() error {
	if err := con.Cluster.Validate(); err != nil {
		return err
	}
	if err := con.Transport.Validate(); err != nil {
		return err
	}
	if err := con.MQ.Validate(); err != nil {
		return err
	}
	return nil
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

func (con *ServerConfig) GetCustomerData(key string) *CustomerData {
	for _, v := range con.CustomerDataList {
		if v.Key == key {
			return v
		}
	}
	return nil
}

type AuthSecret struct {
	AccessSecret string
	AccessExpire int64
	Logto        LogtoConfig
	CasDoor      CasDoorConfig
}
type AttachAddress struct {
	Name       string
	Address    string
	Port       int
	SocketPort int
}
type CustomerData struct {
	Key   string
	Value string
}
type LogtoConfig struct {
	ExpectedAudience string
	Issuer           string
	Enable           bool
}

const CONFIGDIR = "/etc/"

var CONFIGDIRPATH = utils.Getpath() + CONFIGDIR

// 初始化SERVER,true表示系统加载中，未运行，false表示系统已运行
var INITSERVER = false

func NewServiceDefaultConfig(servicename string, port int) *ServerConfig {
	var con ServerConfig
	con.Name = servicename
	str := "{\"Name\":\"" + servicename + "\",\"Port\":" + strconv.Itoa(port) + ",\"Host\":\"0.0.0.0\"}"
	conf.LoadConfigFromJsonBytes([]byte(str), &con)
	con.Telemetry.Batcher = "jaeger"
	ip := utils.GetLocalIP()
	con.Log.ServiceName = servicename + "-" + ip
	con.Log.KeepDays = 10
	con.Log.Level = "info"
	//con.Log.Mode = "file"
	//con.Log.Path = "logs/" + servicename
	con.RunIp = ip
	con.Auth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.Auth.AccessExpire = 86400
	con.Auth.Logto = LogtoConfig{
		ExpectedAudience: "",
		Issuer:           "",
	}
	con.Auth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.ManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ManageAuth.AccessExpire = 86400
	con.ManageAuth.Logto = LogtoConfig{
		ExpectedAudience: "",
		Issuer:           "",
	}
	con.Auth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.ServerManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ServerManageAuth.AccessExpire = 86400
	con.ServerManageAuth.Logto = LogtoConfig{
		ExpectedAudience: "",
		Issuer:           "",
	}
	con.Auth.CasDoor = CasDoorConfig{
		Enable:       false,
		YamlFilePath: "",
	}
	con.SocketPort = port + 10000
	con.Debug = false
	con.IsWhiteList = false
	con.WhiteList = make([]string, 0)
	con.CustomerDataList = make([]*CustomerData, 0)
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
	migrateConfig(file)

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
func migrateConfig(file string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return
	}
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) != nil {
		return
	}

	changed := migrateDurations(m) || migrateNullSlices(m)
	if !changed {
		return
	}
	out, err := json.Marshal(m)
	if err != nil {
		return
	}
	os.WriteFile(file, out, 0o666)
}

// migrateNullSlices converts nil JSON values to empty arrays for fields
// whose Go types are slices that must not be nil (e.g. []string).
func migrateNullSlices(m map[string]interface{}) bool {
	changed := false
	for _, key := range []string{"PrivateKeys", "Endpoints", "Brokers", "NameServers"} {
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
		"DualWriteDuration":
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
	return os.WriteFile(file, utils.String2Bytes(string(out)), 0o777)
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
func (con *ServerConfig) SetAttachService(name string, address string, port, socketport int) {
	if con.AttachServices == nil {
		con.AttachServices = make(map[string]*AttachAddress)
	}
	as, ok := con.AttachServices[name]
	if ok {
		as.Address = address
		as.Port = port
		as.SocketPort = socketport
	} else {
		as = &AttachAddress{
			Name:       name,
			Address:    address,
			Port:       port,
			SocketPort: socketport,
		}
		con.AttachServices[name] = as
	}
}
