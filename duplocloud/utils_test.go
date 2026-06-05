package duplocloud

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_listToStrings(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"two elements", []string{"a", "b"}, []string{"a", "b"}},
		{"single element", []string{"scope-123"}, []string{"scope-123"}},
		{"empty", []string{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := stringsToList(tt.input)
			got := listToStrings(list)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func Test_stringsToList(t *testing.T) {
	t.Run("roundtrip preserves values", func(t *testing.T) {
		input := []string{"scope-aaa", "scope-bbb", "scope-ccc"}
		list := stringsToList(input)
		if list.IsNull() || list.IsUnknown() {
			t.Fatal("expected non-null, non-unknown list")
		}
		if len(list.Elements()) != 3 {
			t.Errorf("Elements len = %d, want 3", len(list.Elements()))
		}
	})

	t.Run("empty slice produces empty list", func(t *testing.T) {
		list := stringsToList([]string{})
		if list.IsNull() || list.IsUnknown() {
			t.Fatal("expected non-null list for empty input")
		}
		if len(list.Elements()) != 0 {
			t.Errorf("expected empty list, got %d elements", len(list.Elements()))
		}
	})

	t.Run("nil slice produces empty list", func(t *testing.T) {
		list := stringsToList(nil)
		if list.IsNull() || list.IsUnknown() {
			t.Fatal("expected non-null list for nil input")
		}
	})

	t.Run("elements are types.String", func(t *testing.T) {
		list := stringsToList([]string{"hello"})
		s, ok := list.Elements()[0].(types.String)
		if !ok {
			t.Fatal("element is not types.String")
		}
		if s.ValueString() != "hello" {
			t.Errorf("value = %q, want %q", s.ValueString(), "hello")
		}
	})
}
