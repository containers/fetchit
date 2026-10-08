package engine

import (
	"errors"
	"fmt"
	"io"
)

// HTTPStatusError reports a failed download without exposing URL credentials.
type HTTPStatusError struct {
	Kind       string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s download returned HTTP %d", e.Kind, e.StatusCode)
}

// copyAndClose checks delayed write errors before callers consume the file.
func copyAndClose(destination io.WriteCloser, source io.Reader) error {
	_, copyErr := io.Copy(destination, source)
	return errors.Join(copyErr, destination.Close())
}
