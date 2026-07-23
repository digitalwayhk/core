package models

import (
	"errors"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// NewValidationError 创建可安全公开的中文校验错误。
func NewValidationError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindValidation, servertypes.PublicCodeValidation, message, errors.New(message))
}

// NewBusinessError 创建可安全公开的中文业务错误。
func NewBusinessError(message string) error {
	return servertypes.NewPublicError(servertypes.ErrorKindBusiness, servertypes.PublicCodeBusiness, message, errors.New(message))
}
