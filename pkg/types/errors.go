package types

import "errors"

var (
	// ErrNotFound indicates a requested node, document, chunk, or edge does not exist.
	ErrNotFound = errors.New("contextgraph: resource not found")

	// ErrUnauthorized indicates the calling PrincipalSet is not permitted to access the resource.
	ErrUnauthorized = errors.New("contextgraph: principal not authorized")

	// ErrInvalidInput indicates a malformed or empty mandatory parameter.
	ErrInvalidInput = errors.New("contextgraph: invalid input")

	// ErrDimensionMismatch indicates a vector's length does not match the configured index dimension.
	ErrDimensionMismatch = errors.New("contextgraph: vector dimension mismatch")

	// ErrInvalidRelation indicates an edge relation is not in the schema allowlist.
	ErrInvalidRelation = errors.New("contextgraph: invalid or disallowed edge relation")

	// ErrEngineClosed indicates an operation was attempted after Close().
	ErrEngineClosed = errors.New("contextgraph: engine is closed")
)
