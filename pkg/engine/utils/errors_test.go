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
