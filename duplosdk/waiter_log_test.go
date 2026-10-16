package duplosdk

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// The waiter logs through tflog on the request context, so its lines reach
// Terraform's provider log (TF_LOG_PROVIDER) with the request's fields.
func TestWaiterLogsThroughTflog(t *testing.T) {
	var out bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &out)
	w := &Waiter[map[string]any]{
		PollInterval: time.Millisecond,
		SuccessState: "Complete",
		StatusFn:     func(m *map[string]any) string { return (*m)["status"].(string) },
	}
	if _, err := w.Wait(ctx, "obj-1", time.Second, func() (*map[string]any, ClientError) {
		return &map[string]any{"status": "Complete"}, nil
	}); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	logs := out.String()
	for _, want := range []string{`waiter(obj-1): start`, `waiter(obj-1): status=Complete`, `"@level":"trace"`} {
		if !strings.Contains(logs, want) {
			t.Errorf("provider log missing %q; got:\n%s", want, logs)
		}
	}
}
