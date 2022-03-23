package utils

import (
	"errors"
	"testing"
)

func TestWrapErr(t *testing.T) {
	msg := "Error"
	e := errors.New("other_err")
	err := WrapErr(e, msg).Error()
	expected := "Error: other_err"
	if err != expected {
		t.Fatalf("Failed: err: %s != %s", err, expected)
	}

	msg = "Error %s"
	e = errors.New("other_err")
	err = WrapErr(e, msg, "test").Error()
	expected = "Error test: other_err"
	if err != expected {
		t.Fatalf("Failed: err: %s != %s", err, expected)
	}
}
