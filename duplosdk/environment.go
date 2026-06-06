package duplosdk

import "net/http"

// Environment API endpoint — the single source of truth for this resource's
// URIs and verbs. The Terraform schema lives in the JSON spec; every path the
// provider calls is defined here, spelled out per operation.
//
// Swagger: /v1/aiservicedesk/user/data/workspaces/{workspaceId}/environments
// The {workspace_id} placeholder is substituted with the attribute value, so
// the emitted URL matches the API regardless of the token's spelling.
func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environments"

	RegisterEndpoint("environment", Endpoint{
		UriBase: base,
		Create:  Operation{Verb: http.MethodPost, Path: ""},        // POST   {base}
		Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},    // GET    {base}/{id}
		Update:  Operation{Verb: http.MethodPut, Path: "/{id}"},    // PUT    {base}/{id}
		Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"}, // DELETE {base}/{id}
	})
}
