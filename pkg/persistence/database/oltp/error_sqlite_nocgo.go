//go:build !cgo

package oltp

func isSQLiteUniqueConstraintError(error) bool {
	return false
}
