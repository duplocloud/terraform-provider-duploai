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
	// body is the raw error response body, kept (never logged from here) so an
	// endpoint's NotFoundResponses can recognise an API-specific "gone" answer.
	body string
}

func newClientError(status int, cause error) ClientError {
	return &clientError{status: status, cause: cause}
}

// newResponseError is newClientError for a non-2xx HTTP response, keeping the
// response body for Endpoint.asNotFound.
func newResponseError(status int, cause error, body string) ClientError {
	return &clientError{status: status, cause: cause, body: body}
}

// errorBody returns the raw response body behind err, or "" when err did not
// come from an HTTP error response.
func errorBody(err ClientError) string {
	if ce, ok := err.(*clientError); ok {
		return ce.body
	}
	return ""
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
