package diff

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"driftguard/pkg/cloud/aws"
	dg_hcl "driftguard/pkg/hcl"

	"github.com/zclconf/go-cty/cty"
)

const remediateFixture = `resource "aws_s3_bucket" "logs" {
  bucket = "acme-logs"
  acl    = "private"

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket" "plain" {
  tags = {
    env = "prod"
  }
}
`

func hclRefs(t *testing.T, content string) []dg_hcl.ResourceRef {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := dg_hcl.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

func TestPlanS3AdoptsLiveVersioning(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Versioning: "Suspended", ACL: "private"}}

	plan := PlanS3(refs, live, nil)
	if !plan.HasWork() {
		t.Fatal("expected work")
	}
	want := []dg_hcl.NestedPatch{{
		Resource: "aws_s3_bucket.logs", Nested: "versioning_configuration",
		Attr: "status", Value: cty.StringVal("Suspended"),
	}}
	if !reflect.DeepEqual(plan.NestedPatches, want) {
		t.Fatalf("got %+v want %+v", plan.NestedPatches, want)
	}
	if len(plan.Patches) != 0 {
		t.Fatalf("acl equal, want no patches: %+v", plan.Patches)
	}
}

func TestPlanS3AdoptsLiveACL(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Versioning: "Enabled", ACL: "public-read"}}

	plan := PlanS3(refs, live, nil)
	want := []dg_hcl.Patch{{
		Resource: "aws_s3_bucket.logs", Attr: "acl", Value: cty.StringVal("public-read"),
	}}
	if !reflect.DeepEqual(plan.Patches, want) {
		t.Fatalf("got %+v want %+v", plan.Patches, want)
	}
	if len(plan.NestedPatches) != 0 {
		t.Fatalf("versioning equal, want no nested patches: %+v", plan.NestedPatches)
	}
}

func TestPlanS3UnsetHCLNeverAdopted(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	// "plain" has no status/acl in HCL; live drift must not become a patch.
	live := []aws.BucketState{{Name: "plain", Versioning: "Suspended", ACL: "public-read"}}

	plan := PlanS3(refs, live, nil)
	if plan.HasWork() {
		t.Fatalf("unset HCL must never be adopted: %+v", plan)
	}
}

func TestPlanS3SkipsMissingBucket(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Missing: true}}

	plan := PlanS3(refs, live, nil)
	if plan.HasWork() {
		t.Fatalf("missing bucket must be skipped: %+v", plan)
	}
}

func TestPlanS3ImportsUnmanaged(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Versioning: "Enabled", ACL: "private"}}

	plan := PlanS3(refs, live, []string{"acme-logs", "alpha-bucket", "weird.name-x/y"})
	want := []string{
		"terraform import aws_s3_bucket.unmanaged_alpha_bucket alpha-bucket",
		"terraform import aws_s3_bucket.unmanaged_weird_name_x_y weird.name-x/y",
	}
	if !reflect.DeepEqual(plan.Imports, want) {
		t.Fatalf("got %v want %v", plan.Imports, want)
	}
}

func TestPlanS3PublicPolicyIsNoteNotPatch(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Versioning: "Enabled", ACL: "private", Public: true}}

	plan := PlanS3(refs, live, nil)
	if plan.HasWork() {
		t.Fatalf("public flag must not produce patches: %+v", plan)
	}
	if len(plan.Notes) != 1 {
		t.Fatalf("want one note, got %v", plan.Notes)
	}
}

func TestPlanS3Deterministic(t *testing.T) {
	refs := hclRefs(t, remediateFixture)
	live := []aws.BucketState{{Name: "acme-logs", Versioning: "Suspended", ACL: "public-read"}}

	a := PlanS3(refs, live, []string{"acme-logs", "z-bucket"})
	b := PlanS3(refs, live, []string{"acme-logs", "z-bucket"})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("PlanS3 is not deterministic across runs")
	}
}
