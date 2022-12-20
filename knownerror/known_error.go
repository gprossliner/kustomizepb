package knownerror

import "fmt"

const (
	EC_AppendDifferentNoSlices = 1
)

type KnownError struct {

	// Message will be printed to the user
	Message string

	// Code currently is only for test asserts
	Code int
}

func (knownError *KnownError) Error() string {
	return knownError.Message
}

func NewKnownError(format string, a ...any) *KnownError {
	return &KnownError{Message: fmt.Sprintf(format, a...)}
}

func (ke *KnownError) WithCode(code int) *KnownError {
	return &KnownError{ke.Message, code}
}
