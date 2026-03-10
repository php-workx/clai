package ops

import (
	"database/sql"
	"errors"
	"strings"
)

// nullableString converts an empty string to NULL in SQLite.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// isDuplicateKeyError checks if the error is a duplicate key constraint violation.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "UNIQUE constraint failed") ||
		strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "already exists")
}

// isNoRows checks if the error is sql.ErrNoRows.
func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
