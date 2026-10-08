package utils

import (
	"errors"
	"testing"
)

func TestWrapErr(t *testing.T) {
	e := errors.New("other_err")
	err := WrapErr(e, "Error").Error()
	expected := "Error: other_err"
	if err != expected {
		t.Fatalf("Failed: err: %s != %s", err, expected)
	}

	e = errors.New("other_err")
	err = WrapErr(e, "Error %s", "test").Error()
	expected = "Error test: other_err"
	if err != expected {
		t.Fatalf("Failed: err: %s != %s", err, expected)
	}
}

func TestWrapErrPreservesCause(t *testing.T) {
	cause := errors.New("cause")
	wrapped := WrapErr(WrapErr(cause, "inner"), "outer")
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped error lost its cause")
	}
	typed := &testError{}
	var got *testError
	if !errors.As(WrapErr(typed, "context"), &got) || got != typed {
		t.Fatal("wrapped error lost its type")
	}
	if got := WrapErr(nil, "invalid %s", "path").Error(); got != "invalid path" {
		t.Fatalf("nil cause produced malformed error: %s", got)
	}
}

type testError struct{}

func (*testError) Error() string { return "typed cause" }
