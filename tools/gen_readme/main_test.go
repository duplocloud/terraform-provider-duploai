package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildResourceTable(t *testing.T) {
	tests := []struct {
		name  string
		specs []specMeta
		want  string
	}{
		{
			name:  "empty",
			specs: []specMeta{},
			want:  "| Resource | Description |\n|---|---|\n",
		},
		{
			name:  "single entry",
			specs: []specMeta{{Name: "environment", Description: "Manages an environment."}},
			want: "| Resource | Description |\n|---|---|\n" +
				"| [`duploai_environment`](docs/resources/environment.md) | Manages an environment |\n",
		},
		{
			name: "trailing period stripped",
			specs: []specMeta{
				{Name: "plan", Description: "Manages a plan."},
				{Name: "env", Description: "No period here"},
			},
			want: "| Resource | Description |\n|---|---|\n" +
				"| [`duploai_plan`](docs/resources/plan.md) | Manages a plan |\n" +
				"| [`duploai_env`](docs/resources/env.md) | No period here |\n",
		},
		{
			name: "all resources included regardless of dataSource flag",
			specs: []specMeta{
				{Name: "alpha", Description: "Alpha resource.", DataSource: true},
				{Name: "beta", Description: "Beta resource."},
			},
			want: "| Resource | Description |\n|---|---|\n" +
				"| [`duploai_alpha`](docs/resources/alpha.md) | Alpha resource |\n" +
				"| [`duploai_beta`](docs/resources/beta.md) | Beta resource |\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildResourceTable(tc.specs)
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestBuildDataSourceTable(t *testing.T) {
	tests := []struct {
		name  string
		specs []specMeta
		want  string
	}{
		{
			name:  "empty — no specs have dataSource",
			specs: []specMeta{{Name: "env", Description: "Manages an environment."}},
			want:  "| Data Source | Description |\n|---|---|\n",
		},
		{
			name: "only dataSource specs are included",
			specs: []specMeta{
				{Name: "alpha", Description: "Alpha resource.", DataSource: true},
				{Name: "beta", Description: "Beta resource."},
				{Name: "gamma", Description: "Gamma resource.", DataSource: true},
			},
			want: "| Data Source | Description |\n|---|---|\n" +
				"| [`duploai_alpha`](docs/data-sources/alpha.md) | Alpha resource |\n" +
				"| [`duploai_gamma`](docs/data-sources/gamma.md) | Gamma resource |\n",
		},
		{
			name: "trailing period stripped",
			specs: []specMeta{
				{Name: "plan", Description: "Manages a plan.", DataSource: true},
			},
			want: "| Data Source | Description |\n|---|---|\n" +
				"| [`duploai_plan`](docs/data-sources/plan.md) | Manages a plan |\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDataSourceTable(tc.specs)
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestLoadSpecs(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("b_resource.json", `{"name":"b_resource","description":"B desc"}`)
	write("a_resource.json", `{"name":"a_resource","description":"A desc"}`)
	write("no_name.json", `{"description":"no name field"}`)
	write("skip_me.txt", `not json`)
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("subdir", "nested.json"), `{"name":"nested","description":"should be skipped"}`)

	specs, err := loadSpecs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (non-empty name, top-level only), got %d", len(specs))
	}
	if specs[0].Name != "a_resource" || specs[1].Name != "b_resource" {
		t.Errorf("expected sorted [a_resource, b_resource], got [%s, %s]", specs[0].Name, specs[1].Name)
	}
}

func TestLoadSpecsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadSpecs(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadSpecsMissingDir(t *testing.T) {
	_, err := loadSpecs("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for missing directory, got nil")
	}
}

func TestReplaceSection(t *testing.T) {
	tests := []struct {
		name       string
		initial    string
		newContent string
		want       string
		wantErr    bool
	}{
		{
			name:       "basic replacement",
			initial:    "before\n<!-- start -->\nold\n<!-- end -->\nafter\n",
			newContent: "new\n",
			want:       "before\n<!-- start -->\nnew\n<!-- end -->\nafter\n",
		},
		{
			name:       "content before and after preserved",
			initial:    "header\n<!-- S -->\nstale\n<!-- E -->\nfooter",
			newContent: "fresh\n",
			want:       "header\n<!-- S -->\nfresh\n<!-- E -->\nfooter",
		},
		{
			name:    "missing start marker",
			initial: "no markers here",
			wantErr: true,
		},
		{
			name:    "missing end marker",
			initial: "<!-- start -->\nno end",
			wantErr: true,
		},
		{
			name:    "markers out of order",
			initial: "<!-- end -->\n<!-- start -->",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "readme*.md")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tc.initial); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()

			start, end := "<!-- start -->", "<!-- end -->"
			if tc.name == "content before and after preserved" {
				start, end = "<!-- S -->", "<!-- E -->"
			}

			err = replaceSection(f.Name(), start, end, tc.newContent)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, _ := os.ReadFile(f.Name())
			if string(got) != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", string(got), tc.want)
			}
		})
	}
}

func TestReplaceSectionMissingFile(t *testing.T) {
	err := replaceSection("/nonexistent/readme.md", "<!-- s -->", "<!-- e -->", "x")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// A description may span paragraphs — that reads well on the registry page but
// would break a markdown table, where a newline ends the row and strands the
// remaining paragraphs as loose text outside the table.
// These tables are an index, so only the first paragraph is kept — the full
// text stays on the generated docs page.
func TestTableCellFlattensMultilineDescriptions(t *testing.T) {
	got := tableCell("First paragraph.\n\nSecond paragraph.\n\nThird.")
	want := "First paragraph."
	if got != want {
		t.Errorf("tableCell() = %q, want %q", got, want)
	}
	if strings.Contains(got, "\n") {
		t.Error("tableCell() left a newline, which would break the table row")
	}
}

// A paragraph that wraps across source lines must still collapse to one line.
func TestTableCellCollapsesWrappedLines(t *testing.T) {
	got := tableCell("A description that\nwraps across lines.")
	want := "A description that wraps across lines."
	if got != want {
		t.Errorf("tableCell() = %q, want %q", got, want)
	}
}

// An unescaped pipe ends the cell early, silently shifting every later column.
func TestTableCellEscapesPipes(t *testing.T) {
	got := tableCell("Valid values: Standard | Fifo")
	if strings.Contains(strings.ReplaceAll(got, "\\|", ""), "|") {
		t.Errorf("tableCell() left an unescaped pipe: %q", got)
	}
}

// A single-paragraph description must pass through unchanged apart from
// whitespace normalisation, so this fix does not churn the existing table.
func TestTableCellLeavesPlainDescriptionsAlone(t *testing.T) {
	in := "Manages a DuploCloud AI Helpdesk Kubernetes ConfigMap"
	if got := tableCell(in); got != in {
		t.Errorf("tableCell() = %q, want unchanged %q", got, in)
	}
}
