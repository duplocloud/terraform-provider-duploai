// gen_readme regenerates the Resources table in README.md from the embedded
// spec JSON files. It replaces everything between <!-- resources-start --> and
// <!-- resources-end --> with a sorted Markdown table derived from each spec's
// name and description fields.
//
// Usage: go run ./tools/gen_readme  (run from the repo root)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type specMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func main() {
	specs, err := loadSpecs("duplocloud/specs")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen_readme: loading specs:", err)
		os.Exit(1)
	}

	table := buildTable(specs)

	if err := replaceSection("README.md", "<!-- resources-start -->", "<!-- resources-end -->", table); err != nil {
		fmt.Fprintln(os.Stderr, "gen_readme: updating README.md:", err)
		os.Exit(1)
	}

	fmt.Printf("gen_readme: wrote %d resources to README.md\n", len(specs))
}

func loadSpecs(dir string) ([]specMeta, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var specs []specMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var s specMeta
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if s.Name != "" {
			specs = append(specs, s)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs, nil
}

func buildTable(specs []specMeta) string {
	var sb strings.Builder
	sb.WriteString("| Resource | Description |\n")
	sb.WriteString("|---|---|\n")
	for _, s := range specs {
		desc := s.Description
		// Trim trailing period for table consistency.
		desc = strings.TrimRight(desc, ".")
		fmt.Fprintf(&sb, "| [`duploai_%s`](docs/resources/%s.md) | %s |\n", s.Name, s.Name, desc)
	}
	return sb.String()
}

// replaceSection rewrites path, replacing everything between startMarker and
// endMarker (inclusive of the markers themselves) with the markers wrapping
// newContent.
func replaceSection(path, startMarker, endMarker, newContent string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(raw)

	startIdx := strings.Index(content, startMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return fmt.Errorf("markers %q / %q not found or out of order in %s", startMarker, endMarker, path)
	}

	endIdx += len(endMarker)
	replacement := startMarker + "\n" + newContent + endMarker
	updated := content[:startIdx] + replacement + content[endIdx:]

	return os.WriteFile(path, []byte(updated), 0o644)
}
