package utils

import (
	"errors"
	"fmt"
)

// WrapErr adds context while preserving the cause for errors.Is and errors.As.
// A nil cause still creates an error, for callers reporting validation failures.
func WrapErr(e error, msg string, args ...interface{}) error {
	message := fmt.Sprintf(msg, args...)
	if e == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%s: %w", message, e)
}
