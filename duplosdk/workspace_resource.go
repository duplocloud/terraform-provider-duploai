package duplosdk

import (
	"strings"
	"time"
)

// WorkspaceResource is the generic REST client for a spec-driven resource. It
// pairs a registered Endpoint (the URIs/verbs) with a resolved path-parameter
// scope, so callers issue Create/Get/Update/Delete without ever assembling a
// URL. T is the domain type — the engine uses map[string]any.
type WorkspaceResource[T any] struct {
	client   *Client
	endpoint Endpoint
	scope    map[string]string
	waiter   *Waiter[T] // nil for synchronous resources
}

// NewWorkspaceResource binds a client, endpoint, and scope into a resource
// client. scope maps each of the endpoint's path parameters to its value.
func NewWorkspaceResource[T any](c *Client, endpoint Endpoint, scope map[string]string, waiter *Waiter[T]) *WorkspaceResource[T] {
	return &WorkspaceResource[T]{client: c, endpoint: endpoint, scope: scope, waiter: waiter}
}

// Create POSTs req to the create path and returns the created object.
func (r *WorkspaceResource[T]) Create(req *T) (*T, ClientError) {
	var resp apiResponse[T]
	if err := r.client.callAPI(r.endpoint.createVerb(), r.endpoint.createPath(r.scope), req, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Get fetches a single object by id.
func (r *WorkspaceResource[T]) Get(id string) (*T, ClientError) {
	var resp apiResponse[T]
	if err := r.client.callAPI(r.endpoint.readVerb(), r.endpoint.readPath(r.scope, id), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Update writes req to the update path and returns the updated object.
func (r *WorkspaceResource[T]) Update(id string, req *T) (*T, ClientError) {
	var resp apiResponse[T]
	if err := r.client.callAPI(r.endpoint.updateVerb(), r.endpoint.updatePath(r.scope, id), req, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Delete removes the object. A 404 is treated as success (already gone).
func (r *WorkspaceResource[T]) Delete(id string) ClientError {
	err := r.client.callAPI(r.endpoint.deleteVerb(), r.endpoint.deletePath(r.scope, id), nil, nil)
	if err != nil && err.IsNotFound() {
		return nil
	}
	return err
}

// WaitUntilReady polls Get until the configured Waiter signals completion.
func (r *WorkspaceResource[T]) WaitUntilReady(id string, timeout time.Duration) (*T, ClientError) {
	return r.waiter.Wait(r.scopeLabel()+"/"+id, timeout, func() (*T, ClientError) {
		return r.Get(id)
	})
}

// scopeLabel joins the scope values in path-parameter order for log/wait names.
func (r *WorkspaceResource[T]) scopeLabel() string {
	params := r.endpoint.PathParams()
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, r.scope[p])
	}
	return strings.Join(parts, "/")
}
