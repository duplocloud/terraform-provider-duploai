package duplosdk

import "strings"

// TenantPath builds the API path prefix for a tenant-scoped resource.
// e.g. TenantPath("abc123", "workspaces") → "/v1/aistudio/tenant/abc123/workspaces"
func TenantPath(tenantID, resource string) string {
	return "/v1/aistudio/tenant/" + tenantID + "/" + strings.TrimLeft(resource, "/")
}

// ResourcePath builds a path for a global (non-tenant) resource.
func ResourcePath(resource string) string {
	return "/v1/aistudio/" + strings.TrimLeft(resource, "/")
}
