package mq

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// mqResourceName 将 prefix 与业务 subject 映射为 Broker 安全资源名。
func mqResourceName(prefix, value string, maxLength int) string {
	raw := strings.Trim(strings.TrimSpace(prefix)+"."+strings.TrimSpace(value), ".")
	var normalized strings.Builder
	normalized.Grow(len(raw))
	lastReplacement := false
	for _, char := range raw {
		valid := char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' ||
			char == '.' || char == '_' || char == '-'
		if valid {
			normalized.WriteRune(char)
			lastReplacement = false
			continue
		}
		if !lastReplacement {
			normalized.WriteByte('_')
			lastReplacement = true
		}
	}
	name := strings.Trim(normalized.String(), "._-")
	if name == "" {
		name = "core"
	}
	if maxLength <= 0 || len(name) <= maxLength {
		return name
	}
	hash := sha256.Sum256([]byte(raw))
	suffix := "-" + hex.EncodeToString(hash[:6])
	if maxLength <= len(suffix) {
		return suffix[len(suffix)-maxLength:]
	}
	base := strings.TrimRight(name[:maxLength-len(suffix)], "._-")
	if base == "" {
		base = "core"
		if len(base)+len(suffix) > maxLength {
			base = base[:maxLength-len(suffix)]
		}
	}
	return base + suffix
}
