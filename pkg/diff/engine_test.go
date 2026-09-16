package diff

import (
	"reflect"
	"testing"

	"driftguard/pkg/cloud/aws"
)

func TestS3Bucket(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		hclStatus string
		hclACL    string
		live      aws.BucketState
		want      []Finding
	}{
		{
			name:      "versioning drift",
			bucket:    "logs",
			hclStatus: "Enabled",
			live:      aws.BucketState{Name: "logs", Versioning: "Suspended"},
			want: []Finding{{
				ResourceType: "aws_s3_bucket", ResourceName: "logs",
				Field: "versioning_configuration.status", Desired: "Enabled", Actual: "Suspended",
			}},
		},
		{
			name:      "acl drift",
			bucket:    "logs",
			hclACL:    "private",
			live:      aws.BucketState{Name: "logs", ACL: "public-read"},
			want: []Finding{{
				ResourceType: "aws_s3_bucket", ResourceName: "logs",
				Field: "acl", Desired: "private", Actual: "public-read",
			}},
		},
		{
			name:      "no drift when equal",
			bucket:    "logs",
			hclStatus: "Enabled",
			hclACL:    "private",
			live:      aws.BucketState{Name: "logs", Versioning: "Enabled", ACL: "private"},
			want:      nil,
		},
		{
			name:   "missing bucket is one finding",
			bucket: "ghost",
			live:   aws.BucketState{Name: "ghost", Missing: true},
			want: []Finding{{
				ResourceType: "aws_s3_bucket", ResourceName: "ghost",
				Field: "resource", Desired: "present", Actual: "missing",
			}},
		},
		{
			name:      "unset hcl settings never drift",
			bucket:    "logs",
			live:      aws.BucketState{Name: "logs", Versioning: "Suspended", ACL: "public-read"},
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := S3Bucket(tt.bucket, tt.hclStatus, tt.hclACL, tt.live)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{ResourceType: "aws_s3_bucket", ResourceName: "logs",
		Field: "acl", Desired: "private", Actual: "public-read"}
	want := `aws_s3_bucket.logs.acl: hcl="private" live="public-read"`
	if f.String() != want {
		t.Fatalf("got %q want %q", f.String(), want)
	}
}
