package router

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/config"
)

func serverConfigFingerprint(con *config.ServerConfig) (string, error) {
	if con == nil {
		return "", fmt.Errorf("config is nil")
	}
	data, err := json.Marshal(con)
	if err != nil {
		return "", fmt.Errorf("marshal config fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeServerConfig(con *config.ServerConfig) (*config.ServerConfig, string, error) {
	if con == nil {
		return nil, "", fmt.Errorf("config is nil")
	}
	data, err := json.Marshal(con)
	if err != nil {
		return nil, "", fmt.Errorf("clone config: %w", err)
	}
	var cloned config.ServerConfig
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, "", fmt.Errorf("clone config: %w", err)
	}
	cloned.ApplyDefaults()
	if err := cloned.Validate(); err != nil {
		return nil, "", err
	}
	fingerprint, err := serverConfigFingerprint(&cloned)
	if err != nil {
		return nil, "", err
	}
	return &cloned, fingerprint, nil
}
