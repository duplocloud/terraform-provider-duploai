package duplocloud

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/duplocloud/terraform-provider-duploai/duplosdk"
)

// attrByName returns the named attribute of spec s, failing the test if absent.
func attrByName(t *testing.T, s *ResourceSpec, name string) AttributeSpec {
	t.Helper()
	for _, a := range s.Attributes {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("%s has no attribute %q", s.Name, name)
	return AttributeSpec{}
}

// exampleResourceBlocks returns the body of every `resource "<typ>"` block in an
// example file (from its header up to the next top-level block).
func exampleResourceBlocks(t *testing.T, path, typ string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header := regexp.MustCompile(`(?m)^(resource|data|output|variable|locals|module) `)
	src := string(b)
	idx := header.FindAllStringIndex(src, -1)
	var blocks []string
	for i, loc := range idx {
		end := len(src)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		if strings.HasPrefix(src[loc[0]:], `resource "`+typ+`"`) {
			blocks = append(blocks, src[loc[0]:end])
		}
	}
	if len(blocks) == 0 {
		t.Fatalf("%s has no %s resource blocks", path, typ)
	}
	return blocks
}

// nodePoolPoll replays one GET of an AksAgentPool: the platform status and the
// live ARM provisioning state under result.cloudDetails.
func nodePoolPoll(status, provisioningState string) *map[string]any {
	m := map[string]any{
		"status": status,
		"result": map[string]any{"cloudDetails": map[string]any{"provisioningState": provisioningState}},
	}
	return &m
}

// F-20: the platform marks an AKS agent pool Complete as soon as the ARM GET is
// non-null, while ARM still reports provisioningState Creating for ~80s more
// (OBSERVED scratchpad/all/retest/azure_node_pool3/wait.log). The create/update
// wait must hold until ARM reports Succeeded, or the next operation 409s with
// AnotherOperationInProgress.
func TestAzureNodePool_WaitsForArmSucceeded(t *testing.T) {
	s := specByName(t, "azure_node_pool")
	r := &dynamicResource{spec: *s}
	w := r.waiter(0)
	if w == nil {
		t.Fatal("azure_node_pool has no waiter")
	}
	w.PollInterval = time.Millisecond

	// The observed sequence: five polls Complete|Creating, then Complete|Succeeded.
	seq := []string{"Creating", "Creating", "Creating", "Creating", "Creating", "Succeeded"}
	n := 0
	obj, err := w.Wait(context.Background(), "pool", 5*time.Second, func() (*map[string]any, duplosdk.ClientError) {
		ps := seq[min(n, len(seq)-1)]
		n++
		return nodePoolPoll("Complete", ps), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(seq) {
		t.Fatalf("waiter returned after %d polls while ARM was still Creating; want %d (return only once provisioningState=Succeeded)", n, len(seq))
	}
	if got := toStringValue(extractPath(*obj, []string{"result", "cloudDetails", "provisioningState"})); got != "Succeeded" {
		t.Fatalf("returned object provisioningState=%q, want Succeeded", got)
	}
}

// F-20: an ARM operation that ends Failed or Canceled is terminal; the platform
// status stays Complete, so without a failure gate the wait would poll until the
// 45m timeout.
func TestAzureNodePool_ArmFailureEndsWait(t *testing.T) {
	s := specByName(t, "azure_node_pool")
	r := &dynamicResource{spec: *s}
	for _, terminal := range []string{"Failed", "Canceled"} {
		w := r.waiter(0)
		w.PollInterval = time.Millisecond
		n := 0
		_, err := w.Wait(context.Background(), "pool", 2*time.Second, func() (*map[string]any, duplosdk.ClientError) {
			n++
			if n == 1 {
				return nodePoolPoll("Complete", "Creating"), nil
			}
			return nodePoolPoll("Complete", terminal), nil
		})
		if err == nil {
			t.Fatalf("%s: wait succeeded, want a terminal failure", terminal)
		}
		if strings.Contains(err.Error(), "timed out") || n > 2 {
			t.Fatalf("%s: wait polled %d times and ended with %q; want an immediate failure on the 2nd poll", terminal, n, err)
		}
		if !strings.Contains(err.Error(), terminal) {
			t.Errorf("%s: error %q does not name the ARM state", terminal, err)
		}
	}
}

// F-20: the gate must not change what a pool that is already Succeeded does.
func TestAzureNodePool_SucceededReturnsImmediately(t *testing.T) {
	s := specByName(t, "azure_node_pool")
	r := &dynamicResource{spec: *s}
	w := r.waiter(0)
	w.PollInterval = time.Millisecond
	n := 0
	if _, err := w.Wait(context.Background(), "pool", time.Second, func() (*map[string]any, duplosdk.ClientError) {
		n++
		return nodePoolPoll("Complete", "Succeeded"), nil
	}); err != nil || n != 1 {
		t.Fatalf("want immediate success, got err=%v polls=%d", err, n)
	}
}

// F-16 (backend defect, origin/dev AksAgentPoolService.cs:553): a create without
// spec.osSku fails with 400 "Value cannot be null". Until the platform is fixed,
// every shipped example sets os_sku and the attribute documents the failure.
// os_sku itself must stay optional so existing configs keep validating.
func TestAzureNodePool_ExamplesSetOsSku(t *testing.T) {
	for _, b := range exampleResourceBlocks(t, "../examples/resources/duploai_azure_node_pool/resource.tf", "duploai_azure_node_pool") {
		if !regexp.MustCompile(`(?m)^\s*os_sku\s*=`).MatchString(b) {
			t.Errorf("example block does not set os_sku (create fails on the platform without it):\n%s", strings.SplitN(b, "\n", 2)[0])
		}
	}
	s := specByName(t, "azure_node_pool")
	a := attrByName(t, s, "os_sku")
	if a.Required || !a.Optional {
		t.Fatalf("os_sku must stay optional (required would break existing configs): %+v", a)
	}
	if !strings.Contains(a.Description, "currently fails") {
		t.Errorf("os_sku description does not warn that omitting it currently fails on the platform: %q", a.Description)
	}
}

// F-19: the backend (origin/dev AwsLambdaService.ValidateSpecAsync:63-84) accepts
// only S3 packages on create and requires runtime and handler. The example must
// not ship a container-image function, and the docs must not promise one.
func TestAwsLambda_DocsAndExampleOnlyOfferS3Packages(t *testing.T) {
	for _, b := range exampleResourceBlocks(t, "../examples/resources/duploai_aws_lambda/resource.tf", "duploai_aws_lambda") {
		if regexp.MustCompile(`(?m)^\s*image_uri\s*=`).MatchString(b) {
			t.Errorf("example sets image_uri, which the platform rejects on create:\n%s", strings.SplitN(b, "\n", 2)[0])
		}
		for _, req := range []string{"runtime", "handler", "s3_bucket", "s3_key"} {
			if !regexp.MustCompile(`(?m)^\s*` + req + `\s*=`).MatchString(b) {
				t.Errorf("example block does not set %s, which the platform requires on create:\n%s", req, strings.SplitN(b, "\n", 2)[0])
			}
		}
	}
	s := specByName(t, "aws_lambda")
	for _, name := range []string{"runtime", "handler"} {
		if d := attrByName(t, s, name).Description; strings.Contains(strings.ToLower(d), "not required for container") {
			t.Errorf("%s description still says it is optional for container images: %q", name, d)
		}
	}
	if d := attrByName(t, s, "image_uri").Description; !strings.Contains(d, "only S3") {
		t.Errorf("image_uri description does not say the platform currently accepts only S3 packages: %q", d)
	}
}
