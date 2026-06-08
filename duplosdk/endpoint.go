package duplosdk

import (
	"net/http"
	"regexp"
)

// pathParamRe matches {placeholder} tokens in a path template.
var pathParamRe = regexp.MustCompile(`\{(\w+)\}`)

// Endpoint configures the API URIs and HTTP verbs for one resource's CRUD
// operations. Register exactly one per resource — in that resource's own file
// in this package — via RegisterEndpoint. This is the single place to change
// when an API path moves or a resource needs a non-standard verb or path.
//
// Every operation's full URL is UriBase + Operation.Path, so the shared prefix
// is written once and each operation only states its verb and the bit that
// differs.
type Endpoint struct {
	// UriBase is the common path prefix shared by all operations. {placeholder}
	// tokens (anything but {id}) are path parameters resolved at call time from
	// attribute values, and are the source of the resource's scope, e.g.
	//   /v1/aiservicedesk/user/data/workspaces/{workspace_id}/environment/networks
	UriBase string

	// Per-operation verb + path. Each is optional; see Operation for the
	// defaults applied when a field is left empty. {id} (the object id) and any
	// path parameters are substituted into the resolved path.
	Create Operation
	Read   Operation
	Update Operation
	Delete Operation

	// Deprovision, when set, is invoked as a pre-delete step for resources the
	// API refuses to delete while live (e.g. a provisioned cluster must tear down
	// its cloud resources first). Leave it zero for resources whose Delete call
	// handles teardown directly — the delete then proceeds unchanged. There is no
	// default path: a Deprovision step runs only when Path is set explicitly,
	// e.g. Operation{Verb: http.MethodPost, Path: "/{id}/deprovision"}.
	Deprovision Operation
}

// Operation is one CRUD action's HTTP verb and the path appended to the
// endpoint's UriBase.
type Operation struct {
	// Verb is the HTTP method. Empty uses the operation default:
	// Create=POST, Read=GET, Update=PUT, Delete=DELETE.
	Verb string
	// Path is appended to UriBase. Empty uses the operation default:
	// "" for Create (the collection itself), "/{id}" for Read/Update/Delete.
	Path string
}

var endpointRegistry = map[string]Endpoint{}

// RegisterEndpoint records a resource's URI configuration. Call it from an
// init() in the resource's file so the engine can look it up by name.
func RegisterEndpoint(resource string, e Endpoint) {
	endpointRegistry[resource] = e
}

// LookupEndpoint returns the endpoint registered for a resource.
func LookupEndpoint(resource string) (Endpoint, bool) {
	e, ok := endpointRegistry[resource]
	return e, ok
}

// PathParams returns the ordered path-parameter names in UriBase, excluding the
// {id} object placeholder.
func (e Endpoint) PathParams() []string {
	out := []string{}
	for _, m := range pathParamRe.FindAllStringSubmatch(e.UriBase, -1) {
		if m[1] != "id" {
			out = append(out, m[1])
		}
	}
	return out
}

// Resolved verb + path for each operation. Defaults fill in the conventional
// REST shape so a plain resource only needs to set UriBase.
func (e Endpoint) createVerb() string { return orDefault(e.Create.Verb, http.MethodPost) }
func (e Endpoint) readVerb() string   { return orDefault(e.Read.Verb, http.MethodGet) }
func (e Endpoint) updateVerb() string { return orDefault(e.Update.Verb, http.MethodPut) }
func (e Endpoint) deleteVerb() string { return orDefault(e.Delete.Verb, http.MethodDelete) }

func (e Endpoint) createPath(scope map[string]string) string {
	return e.resolve(e.UriBase+e.Create.Path, scope, "")
}
func (e Endpoint) readPath(scope map[string]string, id string) string {
	return e.resolve(e.UriBase+itemPath(e.Read.Path), scope, id)
}
func (e Endpoint) updatePath(scope map[string]string, id string) string {
	return e.resolve(e.UriBase+itemPath(e.Update.Path), scope, id)
}
func (e Endpoint) deletePath(scope map[string]string, id string) string {
	return e.resolve(e.UriBase+itemPath(e.Delete.Path), scope, id)
}

// HasUpdate reports whether the resource supports in-place updates. When the
// Update operation is left unset, the resource is immutable: attribute changes
// force replacement, and the engine never issues an update call.
func (e Endpoint) HasUpdate() bool { return e.Update != (Operation{}) }

// HasDeprovision reports whether a pre-delete deprovision step is configured.
func (e Endpoint) HasDeprovision() bool { return e.Deprovision.Path != "" }

func (e Endpoint) deprovisionVerb() string { return orDefault(e.Deprovision.Verb, http.MethodPost) }
func (e Endpoint) deprovisionPath(scope map[string]string, id string) string {
	return e.resolve(e.UriBase+e.Deprovision.Path, scope, id)
}

// itemPath defaults a read/update/delete path to the conventional "/{id}".
func itemPath(p string) string {
	if p == "" {
		return "/{id}"
	}
	return p
}

// resolve substitutes scope (and optionally an object id) into a template.
func (e Endpoint) resolve(tmpl string, scope map[string]string, id string) string {
	vals := make(map[string]string, len(scope)+1)
	for k, v := range scope {
		vals[k] = v
	}
	if id != "" {
		vals["id"] = id
	}
	return substitute(tmpl, vals)
}

func substitute(tmpl string, vals map[string]string) string {
	return pathParamRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		return vals[pathParamRe.FindStringSubmatch(tok)[1]]
	})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
