package duplosdk

import "net/http"

// Kubernetes Helm repository (Flux HelmRepository) API endpoint — the single
// source of truth for this resource's URIs and verbs. The Terraform schema
// lives in the JSON spec.
func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/K8sHelmRepositories"

	RegisterEndpoint("helm_repository", Endpoint{
		UriBase: base,
		Create:  Operation{Verb: http.MethodPost, Path: ""},        // POST   {base}
		Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},    // GET    {base}/{id}
		Update:  Operation{Verb: http.MethodPut, Path: "/{id}"},    // PUT    {base}/{id}
		Delete:  Operation{Verb: http.MethodDelete, Path: "/{id}"}, // DELETE {base}/{id}
	})
}
