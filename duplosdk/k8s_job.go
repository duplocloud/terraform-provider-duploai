package duplosdk

import "net/http"

func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/K8sJobs"

	RegisterEndpoint("k8s_job", Endpoint{
		UriBase: base,
		Create:  Operation{Verb: http.MethodPost, Path: ""},
		Read:    Operation{Verb: http.MethodGet, Path: "/{id}"},
		// No Update: K8s Job spec.template and selector are immutable in Kubernetes.
		// Any change to inputs forces replacement (forceNew on all attributes).
		Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
		Delete:      Operation{Verb: http.MethodDelete, Path: "/{id}"},
	})
}
