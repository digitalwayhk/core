package oltp

import (
	"errors"

	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

type publicErrorProvider interface {
	PublicErrorContract() servertypes.PublicErrorContract
}

func normalizePersistenceError(err error) error {
	if err == nil {
		return nil
	}

	var provider publicErrorProvider
	if errors.As(err, &provider) {
		return err
	}

	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return newPersistenceConflict(err)
	}
	if isSQLiteUniqueConstraintError(err) {
		return newPersistenceConflict(err)
	}

	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return newPersistenceConflict(err)
	}

	return servertypes.NewPublicError(
		servertypes.ErrorKindInternal,
		servertypes.PublicCodeInternal,
		"internal server error",
		err,
	)
}

func newPersistenceConflict(cause error) error {
	return servertypes.NewPublicError(
		servertypes.ErrorKindConflict,
		servertypes.PublicCodeConflict,
		"record already exists",
		cause,
	)
}
