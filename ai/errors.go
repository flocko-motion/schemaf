package ai

import "fmt"

// ErrorType categorizes AI-related errors.
type ErrorType string

const (
	// ErrTypeAuth indicates authentication failure (invalid API key, etc.).
	ErrTypeAuth ErrorType = "auth"

	// ErrTypeRateLimit indicates rate limiting by the provider.
	ErrTypeRateLimit ErrorType = "rate_limit"

	// ErrTypeInvalidReq indicates an invalid request (malformed, missing fields).
	ErrTypeInvalidReq ErrorType = "invalid_request"

	// ErrTypeServer indicates a server error from the provider.
	ErrTypeServer ErrorType = "server_error"

	// ErrTypeNetwork indicates a network or connection error.
	ErrTypeNetwork ErrorType = "network"
)

// Error represents an AI-related error with type information.
type Error struct {
	// Type categorizes the error.
	Type ErrorType

	// Message provides a human-readable error description.
	Message string

	// Status is the HTTP status code (if applicable).
	Status int

	// Err is the underlying error (if any).
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s error: %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s error: %s", e.Type, e.Message)
}

// Unwrap returns the underlying error for error chain unwrapping.
func (e *Error) Unwrap() error {
	return e.Err
}

// NewAuthError creates an authentication error.
func NewAuthError(msg string) *Error {
	return &Error{
		Type:    ErrTypeAuth,
		Message: msg,
		Status:  401,
	}
}

// NewRateLimitError creates a rate limit error.
func NewRateLimitError(msg string) *Error {
	return &Error{
		Type:    ErrTypeRateLimit,
		Message: msg,
		Status:  429,
	}
}

// NewInvalidRequestError creates an invalid request error.
func NewInvalidRequestError(msg string) *Error {
	return &Error{
		Type:    ErrTypeInvalidReq,
		Message: msg,
		Status:  400,
	}
}

// NewServerError creates a server error.
func NewServerError(msg string, status int) *Error {
	return &Error{
		Type:    ErrTypeServer,
		Message: msg,
		Status:  status,
	}
}

// NewNetworkError creates a network error.
func NewNetworkError(msg string, err error) *Error {
	return &Error{
		Type:    ErrTypeNetwork,
		Message: msg,
		Err:     err,
	}
}
