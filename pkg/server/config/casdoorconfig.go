package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type CasDoorServer struct {
	Endpoint     string `yaml:"endpoint"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	Organization string `yaml:"organization"`
	Application  string `yaml:"application"`
	FrontendURL  string `yaml:"frontend_url"`
}

type CasDoorConfigData struct {
	Certificate string        `yaml:"certificate"`
	Server      CasDoorServer `yaml:"server"`
}

type CasDoorConfig struct {
	Enable        bool
	YamlFilePath  string
	WebhookSecret string
	data          *CasDoorConfigData
}

func (con *CasDoorConfig) GetConfigData() (*CasDoorConfigData, error) {
	if !con.Enable {
		return nil, nil
	}
	if con.YamlFilePath == "" {
		return nil, fmt.Errorf("启用CasDoor,但配置文件路径为空！")
	}
	if con.data == nil {
		data, err := loadCasDoorConfig(con.YamlFilePath)
		if err != nil {
			err := fmt.Errorf("加载CasDoor配置文件失败:%v", err)
			return nil, err
		}
		con.data = data
	}
	return con.data, nil
}
func (con *CasDoorConfig) ReloadConfig() error {
	con.data = nil
	data, err := con.GetConfigData()
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	return con.validateLoaded(data)
}

func (con *CasDoorConfig) validateLoaded(data *CasDoorConfigData) error {
	server := data.Server
	if strings.TrimSpace(server.Endpoint) == "" || strings.TrimSpace(server.ClientID) == "" ||
		strings.TrimSpace(server.ClientSecret) == "" || strings.TrimSpace(data.Certificate) == "" ||
		strings.TrimSpace(server.Organization) == "" || strings.TrimSpace(server.Application) == "" {
		return errors.New("CasDoor配置缺少必需字段")
	}
	endpoint := server.Endpoint
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
		data.Server.Endpoint = endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("CasDoor endpoint无效: %q", server.Endpoint)
	}
	if parsed.Scheme != "https" && !isLoopbackCasdoorHost(parsed.Hostname()) {
		return errors.New("生产CasDoor endpoint必须使用HTTPS")
	}
	if subtleSecretEqual(con.WebhookSecret, server.ClientSecret) {
		return errors.New("CasDoor WebhookSecret不能与ClientSecret相同")
	}
	return nil
}

func isLoopbackCasdoorHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func loadCasDoorConfig(configPath string) (*CasDoorConfigData, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	var cfg CasDoorConfigData
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
