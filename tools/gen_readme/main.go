// gen_readme regenerates the Resources and Data Sources tables in README.md
// from the embedded spec JSON files. It replaces everything between
// <!-- resources-start --> / <!-- resources-end --> and between
// <!-- data-sources-start --> / <!-- data-sources-end --> with sorted Markdown
// tables derived from each spec's name, description, and dataSource fields.
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
	Name           string `json:"name"`
	Description    string `json:"description"`
	DataSource     bool   `json:"dataSource,omitempty"`
	DataSourceOnly bool   `json:"dataSourceOnly,omitempty"`
}

func main() {
	specs, err := loadSpecs("duplocloud/specs")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen_readme: loading specs:", err)
		os.Exit(1)
	}

	resourceTable := buildResourceTable(specs)
	if err := replaceSection("README.md", "<!-- resources-start -->", "<!-- resources-end -->", resourceTable); err != nil {
		fmt.Fprintln(os.Stderr, "gen_readme: updating resources section:", err)
		os.Exit(1)
	}

	dsTable := buildDataSourceTable(specs)
	if err := replaceSection("README.md", "<!-- data-sources-start -->", "<!-- data-sources-end -->", dsTable); err != nil {
		fmt.Fprintln(os.Stderr, "gen_readme: updating data-sources section:", err)
		os.Exit(1)
	}

	resourceCount := 0
	dsCount := 0
	for _, s := range specs {
		if !s.DataSourceOnly {
			resourceCount++
		}
		if s.DataSource || s.DataSourceOnly {
			dsCount++
		}
	}
	fmt.Printf("gen_readme: wrote %d resources and %d data sources to README.md\n", resourceCount, dsCount)
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

// tableCell reduces a spec description to something safe for a single markdown
// table cell. A description may span paragraphs — that renders well on the
// registry page, but in a table a newline ends the row and strands the rest as
// loose text outside it, and an unescaped pipe ends the cell early. Take only
// the first paragraph (these tables are an index, not the full reference),
// collapse whitespace runs to single spaces, and escape any pipe.
func tableCell(desc string) string {
	if i := strings.Index(desc, "\n\n"); i >= 0 {
		desc = desc[:i]
	}
	return strings.ReplaceAll(strings.Join(strings.Fields(desc), " "), "|", "\\|")
}

func buildResourceTable(specs []specMeta) string {
	var sb strings.Builder
	sb.WriteString("| Resource | Description |\n")
	sb.WriteString("|---|---|\n")
	for _, s := range specs {
		if s.DataSourceOnly {
			continue
		}
		desc := strings.TrimRight(tableCell(s.Description), ".")
		fmt.Fprintf(&sb, "| [`duploai_%s`](docs/resources/%s.md) | %s |\n", s.Name, s.Name, desc)
	}
	return sb.String()
}

func buildDataSourceTable(specs []specMeta) string {
	var sb strings.Builder
	sb.WriteString("| Data Source | Description |\n")
	sb.WriteString("|---|---|\n")
	for _, s := range specs {
		if !s.DataSource && !s.DataSourceOnly {
			continue
		}
		desc := strings.TrimRight(tableCell(s.Description), ".")
		fmt.Fprintf(&sb, "| [`duploai_%s`](docs/data-sources/%s.md) | %s |\n", s.Name, s.Name, desc)
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
