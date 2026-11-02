package duplocloud

import (
	"sort"
	"testing"
)

// The backend strips duplocloud.ai/* keys from a Kubernetes passthrough
// object's metadata.labels before applying it. A user who sets one sees it
// silently dropped: an update planned after every refresh, or — on a forceNew
// map such as k8s_job.labels — a replacement. reservedKeyPrefixes turns that
// into a plan-time warning. Every spec that writes the object's metadata labels
// must carry it, so a new Kubernetes resource cannot ship without it.
func TestReservedLabelPrefix_OnEveryK8sMetadataLabelsMap(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatalf("loadResourceSpecs: %v", err)
	}
	var checked, missing []string
	for _, spec := range specs {
		for _, a := range spec.Attributes {
			if a.Type != "map(string)" {
				continue
			}
			path := a.RequestPath
			if path == "" {
				path = a.APIPath
			}
			if path != "spec.k8sResource.metadata.labels" {
				continue
			}
			checked = append(checked, spec.Name+"."+a.Name)
			ok := false
			for _, p := range a.ReservedKeyPrefixes {
				if p == "duplocloud.ai/" {
					ok = true
				}
			}
			if !ok {
				missing = append(missing, spec.Name+"."+a.Name)
			}
		}
	}
	sort.Strings(missing)
	if len(checked) == 0 {
		t.Fatal("no spec writes spec.k8sResource.metadata.labels — the guard is matching nothing")
	}
	for _, m := range missing {
		t.Errorf("%s writes spec.k8sResource.metadata.labels without reservedKeyPrefixes [\"duplocloud.ai/\"]", m)
	}
}
