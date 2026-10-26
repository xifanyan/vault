package vault

import "errors"

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
	ErrAlreadyExists   = errors.New("already exists")
	ErrAuthentication  = errors.New("authentication failed")
	ErrInvalidFormat   = errors.New("invalid format")
	ErrLimitExceeded   = errors.New("limit exceeded")
)
