package diff

import (
	"reflect"
	"testing"

	"driftguard/pkg/cloud/aws"
	dg_hcl "driftguard/pkg/hcl"
)

// refsFor parses the shared remediate fixture and returns refs whose File
// points at a stable synthetic name (t.TempDir paths vary run to run, which
// would make DeepEqual expectations unstable).
func refsFor(t *testing.T) []dg_hcl.ResourceRef {
	t.Helper()
	refs := hclRefs(t, remediateFixture)
	for i := range refs {
		refs[i].File = "main.tf"
	}
	return refs
}

func TestS3FindingsByEffectiveBucketName(t *testing.T) {
	refs := refsFor(t)
	live := map[string]aws.BucketState{
		"acme-logs": {Name: "acme-logs", Versioning: "Suspended", ACL: "private"},
		"plain":     {Name: "plain"},
	}
	got := S3Findings(refs, live)
	want := []Finding{{
		ResourceType: "aws_s3_bucket", ResourceName: "logs", // label, not "acme"
		Field: "versioning_configuration.status", Desired: "Enabled", Actual: "Suspended",
		File: "main.tf", Line: 1,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestS3FindingsEnrichesFileLine(t *testing.T) {
	refs := refsFor(t)
	live := map[string]aws.BucketState{
		"acme-logs": {Name: "acme-logs", Versioning: "Enabled", ACL: "public-read"},
	}
	got := S3Findings(refs, live)
	if len(got) != 1 || got[0].Field != "acl" || got[0].File == "" || got[0].Line == 0 {
		t.Fatalf("acl finding not enriched: %+v", got)
	}
	if got[0].ResourceName != "logs" || got[0].File != "main.tf" || got[0].Line != 1 {
		t.Fatalf("got %+v", got[0])
	}
}

func TestS3FindingsNoDriftAndUnknownLiveSkipped(t *testing.T) {
	refs := refsFor(t)
	live := map[string]aws.BucketState{
		"acme-logs": {Name: "acme-logs", Versioning: "Enabled", ACL: "private"}, // no drift
		// "plain" absent from live map: skipped, not fabricated.
	}
	if got := S3Findings(refs, live); got != nil {
		t.Fatalf("want no findings, got %+v", got)
	}
}

func TestS3FindingsMissingBucket(t *testing.T) {
	refs := refsFor(t)
	live := map[string]aws.BucketState{
		"acme-logs": {Name: "acme-logs", Missing: true},
	}
	got := S3Findings(refs, live)
	want := []Finding{{
		ResourceType: "aws_s3_bucket", ResourceName: "logs",
		Field: "resource", Desired: "present", Actual: "missing",
		File: "main.tf", Line: 1,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
