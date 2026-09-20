package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// wrapNotFound turns a bare sql.ErrNoRows into ErrNotFound with a bit of
// context, and passes any other error through wrapped in its own message.
func wrapNotFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}

	return fmt.Errorf("looking up %s: %w", what, err)
}

// checkRowsAffected turns "the WHERE clause matched nothing" into
// ErrNotFound for UPDATE/DELETE statements, where sql.ErrNoRows doesn't
// apply the way it does to a query.
func checkRowsAffected(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for %s: %w", what, err)
	}

	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}

	return nil
}
