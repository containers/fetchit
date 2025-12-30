package unit

import (
	"errors"
	"strings"
	"testing"

	"github.com/containers/fetchit/pkg/engine/utils"
)

// TestWrapErrBasic tests basic error wrapping
func TestWrapErrBasic(t *testing.T) {
	baseErr := errors.New("base error")
	wrapped := utils.WrapErr(baseErr, "operation failed")

	if wrapped == nil {
		t.Fatal("WrapErr returned nil")
	}

	errStr := wrapped.Error()
	if !strings.Contains(errStr, "operation failed") {
		t.Fatalf("Expected error to contain 'operation failed', got: %s", errStr)
	}

	if !strings.Contains(errStr, "base error") {
		t.Fatalf("Expected error to contain 'base error', got: %s", errStr)
	}
}

// TestWrapErrWithFormatting tests error wrapping with formatting
func TestWrapErrWithFormatting(t *testing.T) {
	baseErr := errors.New("file not found")
	filename := "/etc/config.yaml"
	wrapped := utils.WrapErr(baseErr, "failed to read file %s", filename)

	errStr := wrapped.Error()
	if !strings.Contains(errStr, filename) {
		t.Fatalf("Expected error to contain '%s', got: %s", filename, errStr)
	}

	if !strings.Contains(errStr, "file not found") {
		t.Fatalf("Expected error to contain 'file not found', got: %s", errStr)
	}
}

// TestWrapErrMultipleArgs tests error wrapping with multiple formatting args
func TestWrapErrMultipleArgs(t *testing.T) {
	baseErr := errors.New("connection refused")
	host := "localhost"
	port := 8080
	wrapped := utils.WrapErr(baseErr, "failed to connect to %s:%d", host, port)

	errStr := wrapped.Error()
	if !strings.Contains(errStr, "localhost") {
		t.Fatalf("Expected error to contain 'localhost', got: %s", errStr)
	}

	if !strings.Contains(errStr, "8080") {
		t.Fatalf("Expected error to contain '8080', got: %s", errStr)
	}
}

// TestWrapErrNilError tests that wrapping nil error still creates an error
func TestWrapErrNilError(t *testing.T) {
	wrapped := utils.WrapErr(nil, "error context")

	// WrapErr doesn't check for nil, so it will wrap it anyway
	if wrapped == nil {
		t.Fatal("Expected non-nil error when wrapping nil")
	}

	errStr := wrapped.Error()
	if !strings.Contains(errStr, "error context") {
		t.Fatalf("Expected error to contain 'error context', got: %s", errStr)
	}
}

// TestWrapErrChaining tests chaining multiple error wraps
func TestWrapErrChaining(t *testing.T) {
	baseErr := errors.New("disk full")
	wrapped1 := utils.WrapErr(baseErr, "failed to write data")
	wrapped2 := utils.WrapErr(wrapped1, "backup operation failed")

	errStr := wrapped2.Error()
	if !strings.Contains(errStr, "disk full") {
		t.Fatalf("Expected error chain to contain 'disk full', got: %s", errStr)
	}

	if !strings.Contains(errStr, "backup operation failed") {
		t.Fatalf("Expected error chain to contain 'backup operation failed', got: %s", errStr)
	}
}
