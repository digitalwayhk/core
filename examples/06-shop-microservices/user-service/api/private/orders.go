package private

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func trustedUser(req servertypes.IRequest, requireEnabled bool) (*models.User, error) {
	if req == nil {
		return nil, errors.New("用户身份无效")
	}
	uid, _ := req.GetUser()
	user, err := models.FindUser(strings.TrimSpace(uid))
	if err != nil || user == nil {
		return nil, errors.New("用户身份无效")
	}
	if requireEnabled && !user.Enabled {
		return nil, errors.New("用户已禁用，只允许查看")
	}
	return user, nil
}
