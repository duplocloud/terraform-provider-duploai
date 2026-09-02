package duplosdk

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

	// callTimeout, when > 0, overrides the client's default per-request deadline
	// for the operation-initiating calls (create/update/deprovision/delete). Set
	// it via WithCallTimeout.
	callTimeout time.Duration
}

// WithCallTimeout returns a copy of the resource whose operation-initiating
// calls use the given per-request deadline instead of the client default.
//
// The engine passes the operation's own timeout — the same value the waiter
// polls for — so a create/update/delete whose backend work takes minutes is not
// cut off after the default 60s. Without this the waiter would be patient while
// the call that starts it was not, and on a synchronous backend the client
// disconnect cancels the work rather than just losing the response.
func (r *RESTResource[T]) WithCallTimeout(d time.Duration) *RESTResource[T] {
	if d <= 0 {
		return r
	}
	c := *r
	c.callTimeout = d
	return &c
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
	// Reads keep the client default: the waiter polls on the same (possibly
	// widened) resource, and a wedged GET should retry on the next poll rather
	// than hold the whole operation's deadline open.
	return r.decodeWithTimeout(0, verb, path, req)
}

// decodeWithTimeout is decode under an explicit per-request deadline. Used by
// the operation-initiating calls, which may legitimately block for minutes.
func (r *RESTResource[T]) decodeWithTimeout(timeout time.Duration, verb, path string, req *T) (*T, ClientError) {
	var resp apiResponse[T]
	if err := r.client.callAPIWithTimeout(timeout, verb, path, req, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, newClientError(0, fmt.Errorf("%s %s: empty or unexpected response body (no data)", verb, path))
	}
	return resp.Data, nil
}

// Create POSTs req to the create path and returns the created object.
func (r *RESTResource[T]) Create(req *T) (*T, ClientError) {
	return r.decodeWithTimeout(r.callTimeout, r.endpoint.createVerb(), r.endpoint.createPath(r.scope), req)
}

// CreateNoContent issues the create call and ignores the response body. Use for
// endpoints that return 200 with no content — an association resource's POST
// (".../workspaces/{id}/scopes/{scopeId}") carries both ids in the path and
// returns nothing, so decode's "no data" check would reject a perfectly good
// response.
func (r *RESTResource[T]) CreateNoContent() ClientError {
	return r.client.callAPIWithTimeout(r.callTimeout, r.endpoint.createVerb(), r.endpoint.createPath(r.scope), nil, nil)
}

// Get fetches a single object by id.
func (r *RESTResource[T]) Get(id string) (*T, ClientError) {
	return r.decode(r.endpoint.readVerb(), r.endpoint.readPath(r.scope, id), nil)
}

// GetPath fetches and decodes an already-resolved absolute path. Use when the
// object to read is not the resource's own path — an association resource reads
// its parent and checks membership, since the mapping itself has no GET.
func (r *RESTResource[T]) GetPath(path string) (*T, ClientError) {
	return r.decode(http.MethodGet, path, nil)
}

// GetCollection fetches the collection at the resource's UriBase — no /{id}
// segment — and returns its elements. Use for a sub-collection the API serves
// only as a whole, where the per-element GET does not exist (see
// EndpointSpec.ReadFromList). Elements are returned raw so the caller can pick
// the one it wants; an empty or absent data array yields no elements and no
// error, since "the collection is empty" is a legitimate answer.
func (r *RESTResource[T]) GetCollection() ([]map[string]any, ClientError) {
	var resp apiResponse[[]map[string]any]
	path := r.endpoint.ResolvePath(r.endpoint.UriBase, r.scope)
	if err := r.client.callAPIWithTimeout(0, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, nil
	}
	return *resp.Data, nil
}

// retryBaseDelay is the initial backoff between GetWithRetry attempts; it
// doubles per attempt up to retryMaxDelay. Vars (not consts) so tests can
// shrink them.
var (
	retryBaseDelay = 1 * time.Second
	retryMaxDelay  = 8 * time.Second
)

// GetWithRetry fetches the object by id, retrying failures that may resolve on
// their own. The backend's read path can lag a create — the object exists but
// is not returned yet — so not-found is retried too, along with throttling
// (429), server errors (5xx), and transport-level failures. Backoff is
// exponential until maxWait elapses; the last error is returned if the object
// never appears. Use for point-in-time reads (data sources); resource refresh
// must keep single-shot Get so a genuine 404 promptly signals deletion.
func (r *RESTResource[T]) GetWithRetry(ctx context.Context, id string, maxWait time.Duration) (*T, ClientError) {
	delay := retryBaseDelay
	deadline := time.Now().Add(maxWait)
	for {
		obj, err := r.Get(id)
		if err == nil || !isRetryableRead(err) || !time.Now().Add(delay).Before(deadline) {
			return obj, err
		}
		log.Printf("[TRACE] GetWithRetry(%s/%s): status %d, retrying in %s", r.scopeLabel(), id, err.Status(), delay)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(delay):
		}
		if delay < retryMaxDelay {
			delay *= 2
		}
	}
}

// isRetryableRead reports whether a failed read may succeed on a later
// attempt: not-found (read-after-create lag), throttling, server errors, or
// transport/decode failures (status 0).
func isRetryableRead(err ClientError) bool {
	s := err.Status()
	return err.IsNotFound() || s == 0 || s == 429 || s >= 500
}

// Update writes req to the update path and returns the updated object.
func (r *RESTResource[T]) Update(id string, req *T) (*T, ClientError) {
	return r.decodeWithTimeout(r.callTimeout, r.endpoint.updateVerb(), r.endpoint.updatePath(r.scope, id), req)
}

// Delete removes the object. A 404 is treated as success (already gone).
func (r *RESTResource[T]) Delete(id string) ClientError {
	err := r.client.callAPIWithTimeout(r.callTimeout, r.endpoint.deleteVerb(), r.endpoint.deletePath(r.scope, id), nil, nil)
	if err != nil && err.IsNotFound() {
		return nil
	}
	return err
}

// Deprovision triggers teardown of the resource's underlying cloud resources
// without removing the record itself. It is the pre-delete step for resources
// the API will not delete while live. A 404 is treated as success (already gone).
func (r *RESTResource[T]) Deprovision(id string) ClientError {
	err := r.client.callAPIWithTimeout(r.callTimeout, r.endpoint.deprovisionVerb(), r.endpoint.deprovisionPath(r.scope, id), map[string]any{}, nil)
	if err != nil && err.IsNotFound() {
		return nil
	}
	return err
}

// WaitUntilDeprovisioned polls Get until the resource reaches the deprovisioned
// state or is gone (404), erroring on a terminal failure state. Use it between
// Deprovision and Delete so the record is in a deletable state before removal.
func (r *RESTResource[T]) WaitUntilDeprovisioned(ctx context.Context, id, state string, timeout time.Duration) ClientError {
	return r.waiter.WaitDeprovisioned(ctx, r.scopeLabel()+"/"+id, state, timeout, func() (*T, ClientError) {
		return r.Get(id)
	})
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
