package consensus

import (
	"errors"
	"fmt"
)

// RuleError is a consensus failure safe to show to a peer or a caller.
type RuleError struct {
	Code string
	Msg  string
}

func (e *RuleError) Error() string {
	if e.Msg == "" {
		return e.Code
	}
	return e.Code + ": " + e.Msg
}

func rule(code, msg string) error {
	return &RuleError{Code: code, Msg: msg}
}

func rulef(code, format string, args ...any) error {
	return &RuleError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// AsRule reports whether err is a consensus RuleError.
func AsRule(err error) (*RuleError, bool) {
	var re *RuleError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}
