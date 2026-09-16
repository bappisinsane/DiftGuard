package diff

import (
	"fmt"
	"strings"

	"driftguard/pkg/cloud/aws"
	dg_hcl "driftguard/pkg/hcl"

	"github.com/zclconf/go-cty/cty"
)

// Plan is a deterministic remediation plan: adopt live cloud values into HCL,
// import unmanaged cloud resources, and note what cannot be auto-remediated.
type Plan struct {
	Patches       []dg_hcl.Patch
	NestedPatches []dg_hcl.NestedPatch
	Appends       []dg_hcl.AppendBlock
	Imports       []string // ready-to-run `terraform import ...` commands
	Notes         []string // drift that is real but not auto-remediable
}

// HasWork reports whether the plan changes anything.
func (p Plan) HasWork() bool {
	return len(p.Patches)+len(p.NestedPatches)+len(p.Appends) > 0
}

// PlanS3 builds the remediation plan for aws_s3_bucket resources: live cloud
// values are adopted into HCL, and account buckets missing from HCL are
// emitted as terraform import commands.
func PlanS3(refs []dg_hcl.ResourceRef, live []aws.BucketState, accountBuckets []string) Plan {
	var plan Plan

	// 1. Adopt live values for managed resources whose settings drifted.
	for _, r := range refs {
		if r.Type != "aws_s3_bucket" {
			continue
		}
		// Match live state by the effective bucket name; patch the HCL resource
		// identified by its label.
		var st *aws.BucketState
		for i := range live {
			if live[i].Name == dg_hcl.BucketName(r) {
				st = &live[i]
				break
			}
		}
		if st == nil || st.Missing {
			continue // deleted in cloud; nothing to adopt (scan already reported it)
		}
		name := r.Name

		// Versioning: HCL present + drifted -> patch nested status to live value.
		hclStatus := dg_hcl.NestedAttrString(r, "versioning_configuration", "status")
		if hclStatus != "" && hclStatus != st.Versioning && st.Versioning != "" {
			plan.NestedPatches = append(plan.NestedPatches, dg_hcl.NestedPatch{
				Resource: "aws_s3_bucket." + name,
				Nested:   "versioning_configuration",
				Attr:     "status",
				Value:    cty.StringVal(st.Versioning),
			})
		}

		// ACL: literal HCL acl + drifted + known live canned value -> patch.
		hclACL := dg_hcl.AttrString(r, "acl")
		if hclACL != "" && st.ACL != "" && hclACL != st.ACL {
			plan.Patches = append(plan.Patches, dg_hcl.Patch{
				Resource: "aws_s3_bucket." + name,
				Attr:     "acl",
				Value:    cty.StringVal(st.ACL),
			})
		}

		// Public access flag: informational only; policy documents are
		// out of scope for automatic rewriting.
		if st.Public && hclACL == "private" {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"aws_s3_bucket.%s: bucket policy grants public access; review the aws_s3_bucket_policy resource manually", name))
		}
	}

	// 2. Unmanaged resources: live buckets with no aws_s3_bucket HCL ref
	// (by label or literal `bucket` attribute) -> terraform import script.
	managed := make(map[string]bool, len(refs))
	for _, r := range refs {
		if r.Type == "aws_s3_bucket" {
			managed[r.Name] = true
			if b := dg_hcl.AttrString(r, "bucket"); b != "" {
				managed[b] = true
			}
		}
	}
	for _, b := range accountBuckets {
		if !managed[b] {
			plan.Imports = append(plan.Imports,
				fmt.Sprintf("terraform import aws_s3_bucket.unmanaged_%s %s", sanitize(b), b))
		}
	}
	return plan
}

func sanitize(s string) string {
	return strings.NewReplacer(".", "_", "-", "_", "/", "_").Replace(s)
}
