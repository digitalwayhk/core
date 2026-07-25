package safe

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/types"
)

const (
	secretArgsClaim       = "secret_args"
	secretEnvelopeVersion = "v1"
	maxSecretClaimCount   = 16
	maxSecretClaimKeyLen  = 64
	maxSecretClaimValue   = 1024
	maxSecretClaimsBytes  = 4096
)

type verifiedSecretClaimsContextKey struct{}

// ConfigureSecretData 使用当前认证域 AccessSecret 准备秘密 Claim 的直接加密上下文。
func (own *Claims) ConfigureSecretData(accessSecret string, authType types.AuthType) error {
	if own == nil || strings.TrimSpace(own.Uid) == "" || strings.TrimSpace(accessSecret) == "" {
		return errors.New("秘密 Claim 加密上下文无效")
	}
	if authType != types.AuthTypeUser && authType != types.AuthTypeManage && authType != types.AuthTypeServerManage {
		return errors.New("秘密 Claim 认证域无效")
	}
	key := deriveSecretClaimKey(accessSecret, own.Uid, authType)
	if len(own.secretArgs) > 0 && (own.secretType != authType || subtle.ConstantTimeCompare(own.secretKey, key) != 1) {
		return own.setSecretError(errors.New("秘密 Claim 不允许更换加密域"))
	}
	if own.secretErr != nil {
		return own.secretErr
	}
	own.secretKey = key
	own.secretType = authType
	if own.secretArgs == nil {
		own.secretArgs = make(map[string]string)
	}
	if own.secretSizes == nil {
		own.secretSizes = make(map[string]int)
	}
	return nil
}

// AddSecretData 立即使用服务端域密钥加密数据，不在 Claims 中保留明文。
func (own *Claims) AddSecretData(key, value string) error {
	if own == nil {
		return errors.New("秘密 Claim 尚未配置加密上下文")
	}
	if len(own.secretKey) == 0 {
		return own.setSecretError(errors.New("秘密 Claim 尚未配置加密上下文"))
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxSecretClaimKeyLen {
		return own.setSecretError(errors.New("秘密 Claim key 无效"))
	}
	if _, reserved := reservedTokenClaims[key]; reserved || key == secretArgsClaim {
		return own.setSecretError(fmt.Errorf("秘密 Claim 不能覆盖保留字段 %q", key))
	}
	if len(value) > maxSecretClaimValue {
		return own.setSecretError(errors.New("秘密 Claim value 过大"))
	}
	if _, exists := own.secretArgs[key]; !exists && len(own.secretArgs) >= maxSecretClaimCount {
		return own.setSecretError(errors.New("秘密 Claim 数量超限"))
	}
	total := len(value)
	for existingKey, existingSize := range own.secretSizes {
		if existingKey != key {
			total += existingSize
		}
	}
	if total > maxSecretClaimsBytes {
		return own.setSecretError(errors.New("秘密 Claim 总容量超限"))
	}
	encrypted, err := encryptSecretClaim(own.secretKey, own.Uid, own.secretType, key, value)
	if err != nil {
		return own.setSecretError(fmt.Errorf("加密秘密 Claim: %w", err))
	}
	own.secretArgs[key] = encrypted
	own.secretSizes[key] = len(value)
	return nil
}

func (own *Claims) setSecretError(err error) error {
	if own.secretErr == nil {
		own.secretErr = err
	}
	return err
}

func (own *Claims) validateSecretContext(accessSecret string, authType types.AuthType) error {
	if own == nil || own.secretErr != nil {
		if own != nil && own.secretErr != nil {
			return own.secretErr
		}
		return nil
	}
	if len(own.secretArgs) == 0 {
		return nil
	}
	expected := deriveSecretClaimKey(accessSecret, own.Uid, authType)
	if own.secretType != authType || len(own.secretKey) != len(expected) || subtle.ConstantTimeCompare(own.secretKey, expected) != 1 {
		return errors.New("秘密 Claim 加密域与 Token 签发域不匹配")
	}
	return nil
}

func deriveSecretClaimKey(accessSecret, uid string, authType types.AuthType) []byte {
	mac := hmac.New(sha256.New, []byte(accessSecret))
	_, _ = mac.Write([]byte("digitalway-core/secret-claims/v1\x00"))
	_, _ = mac.Write([]byte(uid))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(authType))
	return mac.Sum(nil)
}

func encryptSecretClaim(key []byte, uid string, authType types.AuthType, claimKey, value string) (string, error) {
	gcm, err := newSecretGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(value), secretClaimAAD(uid, authType, claimKey))
	payload := append(nonce, ciphertext...)
	return secretEnvelopeVersion + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decryptSecretClaim(key []byte, uid string, authType types.AuthType, claimKey, envelope string) (string, error) {
	parts := strings.SplitN(envelope, ".", 2)
	if len(parts) != 2 || parts[0] != secretEnvelopeVersion {
		return "", errors.New("秘密 Claim 密文版本无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("秘密 Claim 密文编码无效")
	}
	gcm, err := newSecretGCM(key)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("秘密 Claim 密文过短")
	}
	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, secretClaimAAD(uid, authType, claimKey))
	if err != nil {
		return "", errors.New("秘密 Claim 验证失败")
	}
	return string(plaintext), nil
}

func newSecretGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func secretClaimAAD(uid string, authType types.AuthType, claimKey string) []byte {
	return []byte(uid + "\x00" + string(authType) + "\x00" + claimKey)
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func decryptSecretClaims(raw interface{}, accessSecret, uid string, authType types.AuthType) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]interface{})
	if !ok {
		if typed, stringMapOK := raw.(map[string]string); stringMapOK {
			values = make(map[string]interface{}, len(typed))
			for key, value := range typed {
				values[key] = value
			}
		} else {
			return nil, errors.New("秘密 Claim 结构无效")
		}
	}
	if len(values) > maxSecretClaimCount {
		return nil, errors.New("秘密 Claim 数量超限")
	}
	key := deriveSecretClaimKey(accessSecret, uid, authType)
	result := make(map[string]string, len(values))
	total := 0
	for claimKey, rawValue := range values {
		if strings.TrimSpace(claimKey) == "" || len(claimKey) > maxSecretClaimKeyLen {
			return nil, errors.New("秘密 Claim key 无效")
		}
		envelope, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("秘密 Claim %q 密文无效", claimKey)
		}
		value, err := decryptSecretClaim(key, uid, authType, claimKey, envelope)
		if err != nil {
			return nil, fmt.Errorf("解密秘密 Claim %q: %w", claimKey, err)
		}
		if len(value) > maxSecretClaimValue {
			return nil, fmt.Errorf("秘密 Claim %q value 过大", claimKey)
		}
		total += len(value)
		if total > maxSecretClaimsBytes {
			return nil, errors.New("秘密 Claim 总容量超限")
		}
		result[claimKey] = value
	}
	return result, nil
}

// WithVerifiedSecretClaims 将已验签并解密的 Claim 绑定到当前服务端请求。
func WithVerifiedSecretClaims(ctx context.Context, claims map[string]string) context.Context {
	return context.WithValue(ctx, verifiedSecretClaimsContextKey{}, cloneStringMap(claims))
}

// VerifiedSecretClaimsFromContext 返回当前请求的服务端秘密 Claim 快照。
func VerifiedSecretClaimsFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	claims, _ := ctx.Value(verifiedSecretClaimsContextKey{}).(map[string]string)
	return cloneStringMap(claims)
}
