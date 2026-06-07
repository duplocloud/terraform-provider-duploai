package duplosdk

import "net/http"

// AWS EKS node group API endpoint — the single source of truth for this
// resource's URIs and verbs. The Terraform schema lives in the JSON spec; every
// path the provider calls is defined here, spelled out per operation.
func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/AWSEksNodeGroups"

	RegisterEndpoint("node_group", Endpoint{
		UriBase: base,
		Create:  Operation{Verb: http.MethodPost, Path: ""},        // POST   {base}
		Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},    // GET    {base}/{id}
		Update:  Operation{Verb: http.MethodPut, Path: "/{id}"},    // PUT    {base}/{id}
		Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"}, // DELETE {base}/{id}
		// The API rejects a delete while the resource is live, so tear it
		// down first: POST {base}/{id}/deprovision, then wait for the
		// DeProvisioned state (see the spec waiter) before the delete call.
		Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"}, // POST {base}/{id}/deprovision
	})
}
