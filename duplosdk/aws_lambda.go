package duplosdk

import "net/http"

func init() {
	const base = "/v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/AwsLambdas"

	RegisterEndpoint("aws_lambda", Endpoint{
		UriBase:     base,
		Create:      Operation{Verb: http.MethodPost, Path: ""},
		Read:        Operation{Verb: http.MethodGet, Path: "/{id}"},
		Update:      Operation{Verb: http.MethodPut, Path: "/{id}"},
		Delete:      Operation{Verb: http.MethodDelete, Path: "/{id}"},
		Deprovision: Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"},
	})
}
