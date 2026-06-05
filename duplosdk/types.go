package duplosdk

import "fmt"

// ClientError is the error type returned by all SDK methods.
// It carries the HTTP status code so callers can distinguish 404 (gone) from 5xx.
type ClientError interface {
	error
	Status() int
	IsNotFound() bool
}

type clientError struct {
	status int
	cause  error
}

func newClientError(status int, cause error) ClientError {
	return &clientError{status: status, cause: cause}
}

func (e *clientError) Error() string    { return fmt.Sprintf("status %d: %v", e.status, e.cause) }
func (e *clientError) Status() int      { return e.status }
func (e *clientError) IsNotFound() bool { return e.status == 404 }

// apiResponse is the generic {message, data} envelope used by DuploCloud AI API responses.
type apiResponse[T any] struct {
	Message string `json:"message"`
	Data    *T     `json:"data"`
}

// PaginatedList is a generic wrapper for paginated API responses.
type PaginatedList[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
}
