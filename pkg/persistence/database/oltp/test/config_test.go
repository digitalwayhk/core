package test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/stretchr/testify/assert"
)

func TestConfig_MysqlDSN(t *testing.T) {
	cfg := &oltp.Config{
		Host:      "127.0.0.1",
		Port:      3306,
		Username:  "user",
		Password:  "pass",
		Database:  "mydb",
		Charset:   "utf8mb4",
		ParseTime: true,
		Loc:       "Local",
	}
	dsn := cfg.MysqlDSN()
	assert.Equal(t, "user:pass@tcp(127.0.0.1:3306)/mydb?charset=utf8mb4&parseTime=true&loc=Local", dsn)
}

func TestConfig_SetMysqlDSN(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/mydb?charset=utf8mb4&parseTime=true&loc=Local"
	cfg := &oltp.Config{}
	err := cfg.SetMysqlDSN(dsn)
	assert.NoError(t, err)
	assert.Equal(t, "user", cfg.Username)
	assert.Equal(t, "pass", cfg.Password)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 3306, cfg.Port)
	assert.Equal(t, "mydb", cfg.Database)
	assert.Equal(t, "utf8mb4", cfg.Charset)
	assert.Equal(t, true, cfg.ParseTime)
	assert.Equal(t, "Local", cfg.Loc)
}

func TestConfig_SetMysqlDSN_Invalid(t *testing.T) {
	invalidDSNs := []string{
		"userpass@tcp(127.0.0.1:3306)/mydb", // 缺少冒号
		"user:pass@tcp127.0.0.1:3306)/mydb", // 缺少括号
		//"user:pass@tcp(127.0.0.1:3306)/",    // 缺少数据库名
	}
	for _, dsn := range invalidDSNs {
		cfg := &oltp.Config{}
		err := cfg.SetMysqlDSN(dsn)
		assert.Error(t, err)
	}
}
