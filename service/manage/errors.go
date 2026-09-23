package manage

import (
	"errors"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func newRecordIDRequiredError() error {
	return servertypes.NewPublicError(
		servertypes.ErrorKindValidation,
		servertypes.PublicCodeValidation,
		"record id is required",
		errors.New("manage record id is required"),
	)
}

func newRecordNotFoundError() error {
	return servertypes.NewPublicError(
		servertypes.ErrorKindNotFound,
		servertypes.PublicCodeNotFound,
		"record not found",
		errors.New("manage record not found"),
	)
}
