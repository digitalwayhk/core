package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
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
	Enable       bool
	YamlFilePath string
	data         *CasDoorConfigData
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
	data, err := con.GetConfigData()
	if err != nil {
		return err
	}
	casdoorsdk.InitConfig(
		data.Server.Endpoint,
		data.Server.ClientID,
		data.Server.ClientSecret,
		data.Certificate,
		data.Server.Organization,
		data.Server.Application,
	)
	return nil
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
