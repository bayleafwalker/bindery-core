package resolver

import "errors"

var (
	// ErrNotImplemented indicates the resolver scaffolding is present but business logic has not been implemented yet.
	ErrNotImplemented = errors.New("capability resolver not implemented")
)
