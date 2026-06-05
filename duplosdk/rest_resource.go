package duplosdk

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RESTResource is the generic REST client for a spec-driven resource. It pairs a
// registered Endpoint (the URIs/verbs) with a resolved path-parameter scope, so
// callers issue Create/Get/Update/Delete without ever assembling a URL. T is the
// domain type — the engine uses map[string]any.
type RESTResource[T any] struct {
	client   *Client
	endpoint Endpoint
	scope    map[string]string
	waiter   *Waiter[T] // nil for synchronous resources
}

// NewRESTResource binds a client, endpoint, and scope into a resource client.
// scope maps each of the endpoint's path parameters to its value.
func NewRESTResource[T any](c *Client, endpoint Endpoint, scope map[string]string, waiter *Waiter[T]) *RESTResource[T] {
	return &RESTResource[T]{client: c, endpoint: endpoint, scope: scope, waiter: waiter}
}

// decode runs one request and returns the unwrapped Data. A missing Data (the
// API returned data:null, a non-enveloped body, or an empty body) is reported
// as an error rather than a nil pointer the caller would dereference.
func (r *RESTResource[T]) decode(verb, path string, req *T) (*T, ClientError) {
	var resp apiResponse[T]
	if err := r.client.callAPI(verb, path, req, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, newClientError(0, fmt.Errorf("%s %s: empty or unexpected response body (no data)", verb, path))
	}
	return resp.Data, nil
}

// Create POSTs req to the create path and returns the created object.
func (r *RESTResource[T]) Create(req *T) (*T, ClientError) {
	return r.decode(r.endpoint.createVerb(), r.endpoint.createPath(r.scope), req)
}

// Get fetches a single object by id.
func (r *RESTResource[T]) Get(id string) (*T, ClientError) {
	return r.decode(r.endpoint.readVerb(), r.endpoint.readPath(r.scope, id), nil)
}

// Update writes req to the update path and returns the updated object.
func (r *RESTResource[T]) Update(id string, req *T) (*T, ClientError) {
	return r.decode(r.endpoint.updateVerb(), r.endpoint.updatePath(r.scope, id), req)
}

// Delete removes the object. A 404 is treated as success (already gone).
func (r *RESTResource[T]) Delete(id string) ClientError {
	err := r.client.callAPI(r.endpoint.deleteVerb(), r.endpoint.deletePath(r.scope, id), nil, nil)
	if err != nil && err.IsNotFound() {
		return nil
	}
	return err
}

// WaitUntilReady polls Get until the configured Waiter signals completion. The
// context aborts the poll loop promptly when the operation is cancelled.
func (r *RESTResource[T]) WaitUntilReady(ctx context.Context, id string, timeout time.Duration) (*T, ClientError) {
	return r.waiter.Wait(ctx, r.scopeLabel()+"/"+id, timeout, func() (*T, ClientError) {
		return r.Get(id)
	})
}

// WaitUntilGone polls Get until the object no longer exists (a NotFound
// response) or a terminal failure state is reached. Use after an asynchronous
// Delete to confirm deprovisioning completed.
func (r *RESTResource[T]) WaitUntilGone(ctx context.Context, id string, timeout time.Duration) ClientError {
	return r.waiter.WaitGone(ctx, r.scopeLabel()+"/"+id, timeout, func() (*T, ClientError) {
		return r.Get(id)
	})
}

// scopeLabel joins the scope values in path-parameter order for log/wait names.
func (r *RESTResource[T]) scopeLabel() string {
	params := r.endpoint.PathParams()
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, r.scope[p])
	}
	return strings.Join(parts, "/")
}
