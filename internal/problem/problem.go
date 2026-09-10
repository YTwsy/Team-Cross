package problem

import "errors"

type Error struct {
	Code     string `json:"code"`
	Message  string `json:"error"`
	Recovery string `json:"recovery,omitempty"`
}

func (e *Error) Error() string                 { return e.Message }
func New(code, message, recovery string) error { return &Error{code, message, recovery} }
func Describe(err error) *Error {
	var p *Error
	if errors.As(err, &p) {
		return p
	}
	return &Error{Code: "operation_failed", Message: err.Error()}
}
