package config

import (
	"encoding/json"
	"io/ioutil"
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
	//con.Log.Mode = "file"
	//con.Log.Path = "logs/" + servicename
	con.RunIp = ip
	con.Auth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.Auth.AccessExpire = 86400
	con.ManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ManageAuth.AccessExpire = 86400
	con.ServerManageAuth.AccessSecret = uuid.Must(uuid.NewV4()).String()
	con.ServerManageAuth.AccessExpire = 86400
	con.Debug = false
	con.IsWhiteList = false
	con.WhiteList = make([]string, 0)
	con.CustomerDataList = make([]*CustomerData, 0)
	return &con
}
func ReadConfig(servicename string) *ServerConfig {
	file := CONFIGDIRPATH + servicename + ".json"
	if !utils.IsExista(file) {
		return nil
	}
	con := &ServerConfig{}
	conf.MustLoad(file, con)
	return con
}
func (own *ServerConfig) Save() error {
	file := CONFIGDIRPATH + own.Name + ".json"
	if !utils.IsExista(file) {
		_, err := utils.CreateDir("etc")
		if err != nil {
			panic(err)
		}
	}
	if own.CustomerDataList == nil {
		own.CustomerDataList = make([]*CustomerData, 0)
	}
	if own.WhiteList == nil {
		own.WhiteList = make([]string, 0)
	}
	data, err := json.Marshal(own)
	if err != nil {
		return err
	}
	str := string(data)
	expity := 1
	if own.Signature.Expiry > 0 {
		expity = int(own.Signature.Expiry / time.Hour)
	}
	str = strings.Replace(str, "\"Expiry\":"+strconv.Itoa(int(own.Signature.Expiry)), "\"Expiry\":\""+strconv.Itoa(int(expity))+"h\"", -1)
	if own.Signature.PrivateKeys == nil {
		str = strings.Replace(str, "\"PrivateKeys\":null", "\"PrivateKeys\":[]", -1)
	}
	// if own.AttachServices == nil || len(own.AttachServices) == 0 {
	// 	str = strings.Replace(str, "\"AttachServices\":null", "\"AttachServices\":[]", -1)
	// }

	err = ioutil.WriteFile(file, utils.String2Bytes(str), 0777)
	if err != nil {
		return err
	}
	return nil
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
