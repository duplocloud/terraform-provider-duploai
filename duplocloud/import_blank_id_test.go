package duplocloud

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// A blank import ID (`terraform import <addr> ""`) must be reported as an
// invalid import ID naming the expected format. The framework hands
// ImportState a non-nil identity even for an import by ID (null on Terraform
// 1.12; an all-null object is handled the same), and that must not be taken
// for an import by identity (F-26).
func TestImportState_BlankIDReportsInvalidImportID(t *testing.T) {
	h := newQAAdvHarness(t, qaAdvRGHandler)
	cases := map[string]*tfprotov6.ResourceIdentityData{
		"no identity":       nil,
		"all-null identity": h.identity(qaAdvRG, nil),
	}
	for name, ident := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := h.server.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{
				TypeName: qaAdvRG, ID: "", Identity: ident,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := qaAdvErr(resp.Diagnostics)
			if !strings.HasPrefix(got, "Invalid import ID: ") || !strings.Contains(got, `got "".`) {
				t.Errorf("blank ID: want 'Invalid import ID: ... got \"\".', got %q", got)
			}
		})
	}
	if len(h.seen()) != 0 {
		t.Errorf("no backend call expected, got %v", h.seen())
	}
}
