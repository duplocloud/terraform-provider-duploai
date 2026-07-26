package duplocloud

import "testing"

// extractFirstNonEmpty backs AttributeSpec.ResponsePaths: a cloud-agnostic output
// reads from whichever per-cloud path is populated.
func TestExtractFirstNonEmpty(t *testing.T) {
	awsResp := map[string]any{
		"result": map[string]any{"clusterArn": "arn:aws:eks:us-east-1:1:cluster/x"},
	}
	azureResp := map[string]any{
		"result": map[string]any{
			"clusterArn": "", // present but empty on Azure
			"azure":      map[string]any{"clusterId": "/subscriptions/s/.../managedClusters/x"},
		},
	}
	paths := []string{"result.clusterArn", "result.azure.clusterId"}

	if got := extractFirstNonEmpty(awsResp, paths); got != "arn:aws:eks:us-east-1:1:cluster/x" {
		t.Errorf("AWS: expected the ARN, got %v", got)
	}
	if got := extractFirstNonEmpty(azureResp, paths); got != "/subscriptions/s/.../managedClusters/x" {
		t.Errorf("Azure: empty clusterArn should fall through to azure.clusterId, got %v", got)
	}
	// None present → nil.
	if got := extractFirstNonEmpty(map[string]any{"result": map[string]any{}}, paths); got != nil {
		t.Errorf("expected nil when no path matches, got %v", got)
	}

	// Regression: a single path whose real value is "" must be preserved as ""
	// (not dropped to nil), else optional+computed string fields with a legit
	// empty value (e.g. description) show perpetual "\"\" -> null" drift.
	if got := extractFirstNonEmpty(map[string]any{"description": ""}, []string{"description"}); got != "" {
		t.Errorf("single empty-string path should return \"\", got %v", got)
	}
	// All fallback paths empty → return the first present ("" here), not nil.
	if got := extractFirstNonEmpty(map[string]any{"result": map[string]any{"clusterArn": ""}}, paths); got != "" {
		t.Errorf("all-empty fallback should return the first present value \"\", got %v", got)
	}
}

// responsePathList prefers ResponsePaths, else the single responsePath.
func TestResponsePathList(t *testing.T) {
	multi := AttributeSpec{Name: "cluster_id", Type: "string", Computed: true, ResponsePaths: []string{"result.clusterArn", "result.azure.clusterId"}}
	if got := multi.responsePathList(); len(got) != 2 {
		t.Errorf("expected 2 fallback paths, got %v", got)
	}
	single := AttributeSpec{Name: "status", Type: "string", Computed: true, APIPath: "status"}
	if got := single.responsePathList(); len(got) != 1 || got[0] != "status" {
		t.Errorf("expected [status], got %v", got)
	}
	none := AttributeSpec{Name: "x", Type: "string", Optional: true, CreatePath: "spec.x"}
	if got := none.responsePathList(); len(got) != 0 {
		t.Errorf("write-only attr should have no response paths, got %v", got)
	}
}
