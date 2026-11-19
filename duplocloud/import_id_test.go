package duplocloud

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every import ID shown in the shipped examples (examples/resources/*/import.sh,
// both the command and any commented sample) must still pass the up-front
// import check: it may only reject IDs that could never import.
func TestImportIDProblem_ShippedExamplesAccepted(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]*dynamicResource{}
	for _, s := range specs {
		if s.DataSourceOnly {
			continue
		}
		ep, err := s.BuildEndpoint()
		if err != nil {
			t.Fatal(err)
		}
		byType["duploai_"+s.Name] = &dynamicResource{spec: s, endpoint: ep}
	}
	files, _ := filepath.Glob("../examples/resources/*/import.sh")
	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			if len(fields) != 4 || fields[0] != "terraform" || fields[1] != "import" {
				continue
			}
			typ := strings.SplitN(fields[2], ".", 2)[0]
			r, ok := byType[typ]
			if !ok {
				continue
			}
			checked++
			if msg := r.importIDProblem(fields[3]); msg != "" {
				t.Errorf("%s: example import ID %q rejected: %s", f, fields[3], msg)
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d example import IDs found; the example parsing is broken", checked)
	}
}

// Malformed IDs fail up front with the expected format named.
func TestImportIDProblem_Rejects(t *testing.T) {
	specs, err := loadResourceSpecs()
	if err != nil {
		t.Fatal(err)
	}
	var r *dynamicResource
	for _, s := range specs {
		if s.Name == "resource_group" {
			ep, _ := s.BuildEndpoint()
			r = &dynamicResource{spec: s, endpoint: ep}
		}
	}
	if r == nil {
		t.Fatal("resource_group spec not found")
	}
	for id, want := range map[string]string{
		"onlyone":    "<workspace_id>/<id>",
		"ws1/":       "<id> is empty",
		"/abc":       "<workspace_id> is empty",
		"ws1/ abc ":  "",
		"ws1/abc":    "",
		"ws1/abc/xy": "", // extra segments are ignored by parseID today; still accepted
	} {
		got := r.importIDProblem(id)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("importIDProblem(%q) = %q, want containing %q", id, got, want)
		}
	}
}
