package config

import (
	"encoding/json"
	"strings"
)

const redactedConfigValue = "[REDACTED]"

// AdminView returns a JSON-compatible configuration view with credentials and
// private material removed. The regular QueryConfig response remains the full
// internal contract used by service-to-service calls.
func AdminView(con *ServerConfig) (map[string]interface{}, error) {
	data, err := json.Marshal(con)
	if err != nil {
		return nil, err
	}
	var view map[string]interface{}
	if err := json.Unmarshal(data, &view); err != nil {
		return nil, err
	}
	redactConfigMap(view)
	return view, nil
}

// MergeProtectedFields keeps credentials from the current runtime config.
// Configuration editing intentionally cannot rotate authentication material.
func MergeProtectedFields(existing, incoming *ServerConfig) (*ServerConfig, error) {
	oldData, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	newData, err := json.Marshal(incoming)
	if err != nil {
		return nil, err
	}
	var oldMap, newMap map[string]interface{}
	if err := json.Unmarshal(oldData, &oldMap); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(newData, &newMap); err != nil {
		return nil, err
	}
	mergeProtectedMap(oldMap, newMap)
	merged, err := json.Marshal(newMap)
	if err != nil {
		return nil, err
	}
	result := &ServerConfig{}
	if err := json.Unmarshal(merged, result); err != nil {
		return nil, err
	}
	return result, nil
}

func mergeProtectedMap(oldMap, newMap map[string]interface{}) {
	for key, oldValue := range oldMap {
		if isSensitiveConfigKey(key) {
			newMap[key] = oldValue
			continue
		}
		oldChild, oldOK := oldValue.(map[string]interface{})
		newChild, newOK := newMap[key].(map[string]interface{})
		if oldOK && newOK {
			mergeProtectedMap(oldChild, newChild)
		}
	}
}

func redactConfigMap(values map[string]interface{}) {
	for key, value := range values {
		if isSensitiveConfigKey(key) {
			switch value.(type) {
			case string:
				values[key] = redactedConfigValue
			default:
				// Keep the JSON shape compatible with the update DTO while
				// ensuring private-key/password collections never cross the API.
				values[key] = []interface{}{}
			}
			continue
		}
		switch nested := value.(type) {
		case map[string]interface{}:
			redactConfigMap(nested)
		case []interface{}:
			for _, item := range nested {
				if child, ok := item.(map[string]interface{}); ok {
					redactConfigMap(child)
				}
			}
		}
	}
}

func isSensitiveConfigKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
	return strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "privatekey") ||
		strings.Contains(key, "clientsecret") ||
		strings.Contains(key, "webhooksecret")
}
