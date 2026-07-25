package types

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorKindValidation      ErrorKind = "validation"
	ErrorKindUnauthenticated ErrorKind = "unauthenticated"
	ErrorKindForbidden       ErrorKind = "forbidden"
	ErrorKindNotFound        ErrorKind = "not_found"
	ErrorKindConflict        ErrorKind = "conflict"
	ErrorKindBusiness        ErrorKind = "business"
	ErrorKindRateLimited     ErrorKind = "rate_limited"
	ErrorKindUnavailable     ErrorKind = "unavailable"
	ErrorKindInternal        ErrorKind = "internal"
)

const (
	PublicCodeValidation      = 40001
	PublicCodeUnauthenticated = 40100
	PublicCodeForbidden       = 40300
	PublicCodeNotFound        = 40400
	PublicCodeConflict        = 40900
	PublicCodeBusiness        = 42200
	PublicCodeRateLimited     = 42900
	PublicCodeInternal        = 50000
	PublicCodeUnavailable     = 50300
)

type PublicErrorContract struct {
	Kind       ErrorKind
	Code       int
	HTTPStatus int
	Message    string
}

type PublicError struct {
	contract PublicErrorContract
	cause    error
}

func NewPublicError(kind ErrorKind, code int, safeMessage string, cause error) *PublicError {
	contract := defaultPublicErrorContract(kind)
	if code != 0 {
		contract.Code = code
	}
	if safeMessage != "" {
		contract.Message = safeMessage
	}
	return &PublicError{contract: contract, cause: cause}
}

func (e *PublicError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.contract.Kind, e.cause)
	}
	return string(e.contract.Kind)
}

func (e *PublicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *PublicError) PublicErrorContract() PublicErrorContract {
	if e == nil {
		return defaultPublicErrorContract(ErrorKindInternal)
	}
	return e.contract
}

type publicErrorProvider interface {
	PublicErrorContract() PublicErrorContract
}

func ResolvePublicError(err error) PublicErrorContract {
	if err != nil {
		var provider publicErrorProvider
		if errors.As(err, &provider) {
			return provider.PublicErrorContract()
		}
	}
	return defaultPublicErrorContract(ErrorKindInternal)
}

func defaultPublicErrorContract(kind ErrorKind) PublicErrorContract {
	switch kind {
	case ErrorKindValidation:
		return PublicErrorContract{Kind: kind, Code: PublicCodeValidation, HTTPStatus: 400, Message: "invalid request"}
	case ErrorKindUnauthenticated:
		return PublicErrorContract{Kind: kind, Code: PublicCodeUnauthenticated, HTTPStatus: 401, Message: "authentication failed"}
	case ErrorKindForbidden:
		return PublicErrorContract{Kind: kind, Code: PublicCodeForbidden, HTTPStatus: 403, Message: "permission denied"}
	case ErrorKindNotFound:
		return PublicErrorContract{Kind: kind, Code: PublicCodeNotFound, HTTPStatus: 404, Message: "resource not found"}
	case ErrorKindConflict:
		return PublicErrorContract{Kind: kind, Code: PublicCodeConflict, HTTPStatus: 409, Message: "resource conflict"}
	case ErrorKindBusiness:
		return PublicErrorContract{Kind: kind, Code: PublicCodeBusiness, HTTPStatus: 422, Message: "business rule rejected"}
	case ErrorKindRateLimited:
		return PublicErrorContract{Kind: kind, Code: PublicCodeRateLimited, HTTPStatus: 429, Message: "rate limit exceeded"}
	case ErrorKindUnavailable:
		return PublicErrorContract{Kind: kind, Code: PublicCodeUnavailable, HTTPStatus: 503, Message: "service unavailable"}
	default:
		return PublicErrorContract{Kind: ErrorKindInternal, Code: PublicCodeInternal, HTTPStatus: 500, Message: "internal server error"}
	}
}
