package utils

import "fmt"

func WrapErr(e error, msg string, args ...interface{}) error {
	final_msg := fmt.Sprintf(msg, args...)
	return fmt.Errorf("%s: %s", final_msg, e)
}
