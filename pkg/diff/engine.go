// Package diff computes deterministic drift findings between HCL resource
// references and live cloud state. No heuristics: every rule is explicit.
package diff

import (
	"fmt"

	"driftguard/pkg/cloud/aws"
	dg_hcl "driftguard/pkg/hcl"
)

// Field is one Terraform schema field DriftGuard knows how to compare.
type Field struct {
	// Attr is the HCL attribute holding the setting (e.g. "acl").
	Attr string
	// Nested is the optional nested block holding it (e.g. "versioning_configuration").
	Nested string
}

// S3 fields compared for drift. Extend as provider coverage grows.
var s3Fields = []Field{
	{Attr: "versioning", Nested: "versioning_configuration"},
	{Attr: "acl"},
}

// Finding is one drift item: the cloud disagrees with the HCL. Field names
// are the CI JSON contract of `driftguard check`.
type Finding struct {
	ResourceType string `json:"resource_type"` // e.g. "aws_s3_bucket"
	ResourceName string `json:"resource_name"` // HCL label, e.g. "logs"
	Field        string `json:"field"`         // e.g. "versioning_configuration.status"
	Desired      string `json:"desired"`       // what HCL says
	Actual       string `json:"actual"`        // what the cloud returns
	File         string `json:"file,omitempty"` // source .tf file
	Line         int    `json:"line,omitempty"` // source line of the resource block
}

func (f Finding) String() string {
	return fmt.Sprintf("%s.%s: hcl=%q live=%q", f.ResourceType+"."+f.ResourceName, f.Field, f.Desired, f.Actual)
}

// S3Bucket diffs one aws_s3_bucket ResourceRef against live BucketState.
func S3Bucket(bucketName string, hclStatus string, hclACL string, live aws.BucketState) []Finding {
	if live.Missing {
		return []Finding{{
			ResourceType: "aws_s3_bucket",
			ResourceName: bucketName,
			Field:        "resource",
			Actual:       "missing",
			Desired:      "present",
		}}
	}
	var out []Finding
	if hclStatus != "" && hclStatus != live.Versioning {
		out = append(out, Finding{
			ResourceType: "aws_s3_bucket",
			ResourceName: bucketName,
			Field:        "versioning_configuration.status",
			Desired:      hclStatus,
			Actual:       live.Versioning,
		})
	}
	if hclACL != "" && hclACL != live.ACL {
		out = append(out, Finding{
			ResourceType: "aws_s3_bucket",
			ResourceName: bucketName,
			Field:        "acl",
			Desired:      hclACL,
			Actual:       live.ACL,
		})
	}
	return out
}

// S3Findings diffs every aws_s3_bucket ref against live state and enriches
// findings with source file/line. This is the one mapping all commands share:
// scan renders it, check serializes it, remediate plans from the same refs.
func S3Findings(refs []dg_hcl.ResourceRef, live map[string]aws.BucketState) []Finding {
	var out []Finding
	for _, r := range refs {
		if r.Type != "aws_s3_bucket" {
			continue
		}
		l, ok := live[dg_hcl.BucketName(r)]
		if !ok {
			continue
		}
		for _, f := range S3Bucket(r.Name,
			dg_hcl.NestedAttrString(r, "versioning_configuration", "status"),
			dg_hcl.AttrString(r, "acl"), l) {
			f.File = r.File
			f.Line = r.BlockRange.Start.Line
			out = append(out, f)
		}
	}
	return out
}
