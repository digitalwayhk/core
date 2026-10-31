// 本文件验证原生买家认证不接受跨域 Token，也不信任请求体用户身份。
package reference

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

// TestNativeUserTokenDomain 验证有效用户 Token、管理员域、过期和错误签名的边界。
func TestNativeUserTokenDomain(t *testing.T) {
	a := &application{}
	a.file.Auth.AccessSecret = "only-native-unit-test-secret"
	for _, tc := range []struct {
		name, domain, key string
		expiry            int64
		valid             bool
	}{
		{"用户", "auth", a.file.Auth.AccessSecret, time.Now().Add(time.Hour).Unix(), true},
		{"管理域", "manage", a.file.Auth.AccessSecret, time.Now().Add(time.Hour).Unix(), false},
		{"过期", "auth", a.file.Auth.AccessSecret, time.Now().Add(-time.Hour).Unix(), false},
		{"错误签名", "auth", "wrong", time.Now().Add(time.Hour).Unix(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, e := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": "720001", "auth_type": tc.domain, "token_use": "access", "exp": tc.expiry, "iat": time.Now().Add(-time.Minute).Unix()}).SignedString([]byte(tc.key))
			require.NoError(t, e)
			r := httptest.NewRequest("POST", "/api/shop-user/addorder", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			id, e := a.user(r)
			if tc.valid {
				require.NoError(t, e)
				require.EqualValues(t, 720001, id)
			} else {
				require.Error(t, e)
			}
		})
	}
}
