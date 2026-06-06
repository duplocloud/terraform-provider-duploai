package duplosdk

import "net/http"

// Resource group API endpoint — the single source of truth for this
// resource's URIs and verbs. The Terraform schema lives in the JSON spec; every
// path the provider calls is defined here, spelled out per operation.
func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/resource-groups"

	RegisterEndpoint("resource_group", Endpoint{
		UriBase:     base,
		Create:      Operation{Verb: http.MethodPost, Path: ""},                  // POST   {base}
		Read:        Operation{Verb: http.MethodGet, Path: "/{id}"},              // GET    {base}/{id}
		Update:      Operation{Verb: http.MethodPut, Path: "/{id}"},              // PUT    {base}/{id}
		Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"}, // POST   {base}/{id}/deprovision
		Delete:      Operation{Verb: http.MethodDelete, Path: "/{id}"},           // DELETE {base}/{id}
	})
}
