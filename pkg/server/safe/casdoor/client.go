package casdoor

import (
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
)

var (
	ErrIdentityInactive = errors.New("casdoor identity is inactive")
	ErrDomainDisabled   = errors.New("casdoor authentication domain is disabled")
)

// Client 是框架实际使用的 Casdoor SDK 最小能力集合，便于测试替换远程边界。
type Client interface {
	GetOAuthToken(code, state string, opts ...casdoorsdk.OAuthOption) (*oauth2.Token, error)
	ParseJwtToken(token string) (*casdoorsdk.Claims, error)
	GetUser(name string) (*casdoorsdk.User, error)
}

// DomainClient 保存单个认证域的独立 Casdoor Client 和不可变域元数据。
type DomainClient struct {
	client       Client
	organization string
	application  string
}

func (c *DomainClient) Organization() string {
	if c == nil {
		return ""
	}
	return c.organization
}

func (c *DomainClient) Application() string {
	if c == nil {
		return ""
	}
	return c.application
}

func (c *DomainClient) GetOAuthToken(code, state string, opts ...casdoorsdk.OAuthOption) (*oauth2.Token, error) {
	if c == nil || c.client == nil {
		return nil, ErrDomainDisabled
	}
	return c.client.GetOAuthToken(code, state, opts...)
}

func (c *DomainClient) ParseJwtToken(token string) (*casdoorsdk.Claims, error) {
	if c == nil || c.client == nil {
		return nil, ErrDomainDisabled
	}
	return c.client.ParseJwtToken(token)
}

func (c *DomainClient) GetUser(name string) (*casdoorsdk.User, error) {
	if c == nil || c.client == nil {
		return nil, ErrDomainDisabled
	}
	return c.client.GetUser(name)
}

// ClientSet 为 Auth 和 Manage 保存互不共享状态的 Casdoor Client。
type ClientSet struct {
	auth   *DomainClient
	manage *DomainClient
}

func NewClientSet(auth, manage config.CasDoorConfig) (*ClientSet, error) {
	authClient, err := buildDomainClient(&auth)
	if err != nil {
		return nil, fmt.Errorf("初始化Auth Casdoor Client失败: %w", err)
	}
	manageClient, err := buildDomainClient(&manage)
	if err != nil {
		return nil, fmt.Errorf("初始化Manage Casdoor Client失败: %w", err)
	}
	if err := validateCrossDomainSecrets(&auth, &manage); err != nil {
		return nil, err
	}
	return &ClientSet{auth: authClient, manage: manageClient}, nil
}

func (s *ClientSet) Auth() *DomainClient {
	if s == nil {
		return nil
	}
	return s.auth
}

func (s *ClientSet) Manage() *DomainClient {
	if s == nil {
		return nil
	}
	return s.manage
}

func buildDomainClient(cfg *config.CasDoorConfig) (*DomainClient, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	if err := cfg.ReloadConfig(); err != nil {
		return nil, err
	}
	data, err := cfg.GetConfigData()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("Casdoor配置为空")
	}
	server := data.Server
	if err := validateCertificate(data.Certificate); err != nil {
		return nil, err
	}
	client := casdoorsdk.NewClient(
		server.Endpoint,
		server.ClientID,
		server.ClientSecret,
		data.Certificate,
		server.Organization,
		server.Application,
	)
	return &DomainClient{
		client:       client,
		organization: server.Organization,
		application:  server.Application,
	}, nil
}

func validateCrossDomainSecrets(auth, manage *config.CasDoorConfig) error {
	if auth == nil || manage == nil || !auth.Enable || !manage.Enable {
		return nil
	}
	authData, err := auth.GetConfigData()
	if err != nil {
		return err
	}
	manageData, err := manage.GetConfigData()
	if err != nil {
		return err
	}
	if secureEqual(auth.WebhookSecret, manageData.Server.ClientSecret) ||
		secureEqual(manage.WebhookSecret, authData.Server.ClientSecret) {
		return errors.New("Auth和Manage Casdoor WebhookSecret不能复用另一认证域的ClientSecret")
	}
	return nil
}

func secureEqual(left, right string) bool {
	return left != "" && len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validateCertificate(certificate string) error {
	if _, err := jwt.ParseRSAPublicKeyFromPEM([]byte(certificate)); err == nil {
		return nil
	}
	if _, err := jwt.ParseECPublicKeyFromPEM([]byte(certificate)); err == nil {
		return nil
	}
	return errors.New("Casdoor Certificate必须是有效的RSA或ECDSA公钥")
}

// VerifyActiveUser 检查 Casdoor SDK 当前用户是否与已验证的身份域一致。
func VerifyActiveUser(user *casdoorsdk.User, organization, subject string) error {
	if user == nil || user.IsForbidden || user.IsDeleted ||
		user.Owner != organization || user.Name != subject {
		return ErrIdentityInactive
	}
	return nil
}
