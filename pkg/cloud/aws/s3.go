// Package aws fetches live AWS resource state for drift comparison.
package aws

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"golang.org/x/sync/errgroup"
)

// BucketState is the live cloud state of one S3 bucket.
type BucketState struct {
	Name       string
	Versioning string // "Enabled", "Suspended", or "" when unset
	ACL      string // canned-ACL equivalent derived from grants; "" when unknown
	Public   bool   // bucket policy grants public access
	Missing  bool   // API reports NoSuchBucket: resource deleted out-of-band
}

// FetchBucketStates queries versioning and public-access status for each bucket
// name concurrently (max 8 in flight). Errors carry the bucket name. Deleted
// buckets return a BucketState with Missing set instead of an error.
func FetchBucketStates(ctx context.Context, client *s3.Client, names []string) ([]BucketState, error) {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8) // ponytail: fixed bound of 8; make a flag if rate limits bite
	states := make([]BucketState, len(names))
	for i, name := range names {
		g.Go(func() error {
			v, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &name})
			if err != nil {
				var nfe *types.NoSuchBucket
				if errors.As(err, &nfe) {
					states[i] = BucketState{Name: name, Missing: true}
					return nil
				}
				return fmt.Errorf("s3 %s versioning: %w", name, err)
			}
			p, err := client.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{Bucket: &name})
			if err != nil {
				var nfe *types.NoSuchBucket
				if !errors.As(err, &nfe) {
					return fmt.Errorf("s3 %s policy status: %w", name, err)
				}
				p = nil // deleted mid-scan: treat as no policy
			}
			ver := string(v.Status) // "" when versioning never configured
			public := p != nil && p.PolicyStatus != nil && p.PolicyStatus.IsPublic != nil && *p.PolicyStatus.IsPublic
			a, err := client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: &name})
			acl := ""
			if err != nil {
				var ae smithy.APIError
				if !errors.As(err, &ae) || ae.ErrorCode() != "AccessControlListNotSupported" {
					return fmt.Errorf("s3 %s acl: %w", name, err)
				}
				acl = "private" // ACLs disabled (BucketOwnerEnforced): effectively private
			} else {
				acl = deriveACL(a)
			}
			states[i] = BucketState{Name: name, Versioning: ver, ACL: acl, Public: public}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return states, nil
}

// deriveACL maps a bucket's grant list to the equivalent canned-ACL name.
// ponytail: exact-match mapping only; custom grant sets return "" so the diff
// skips the acl check until the Phase 3 schema translation table lands.
func deriveACL(out *s3.GetBucketAclOutput) string {
	var allUsersRead, allUsersWrite, ownerFull bool
	for _, g := range out.Grants {
		if g.Grantee == nil {
			continue
		}
		switch g.Permission {
		case types.PermissionFullControl:
			if g.Grantee.Type == types.TypeCanonicalUser {
				ownerFull = true
			}
		case types.PermissionRead:
			if isAllUsers(g.Grantee) {
				allUsersRead = true
			}
		case types.PermissionWrite:
			if isAllUsers(g.Grantee) {
				allUsersWrite = true
			}
		}
	}
	switch {
	case ownerFull && !allUsersRead && !allUsersWrite:
		return "private"
	case ownerFull && allUsersRead && !allUsersWrite:
		return "public-read"
	case ownerFull && allUsersRead && allUsersWrite:
		return "public-read-write"
	default:
		return ""
	}
}

func isAllUsers(gr *types.Grantee) bool {
	return gr.Type == types.TypeGroup && gr.URI != nil && strings.HasSuffix(*gr.URI, "/AllUsers")
}

// ListBucketNames returns all bucket names in the account, sorted.
func ListBucketNames(ctx context.Context, client *s3.Client) ([]string, error) {
	var names []string
	var token *string
	for {
		out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{ContinuationToken: token})
		if err != nil {
			return nil, fmt.Errorf("listing buckets: %w", err)
		}
		for _, b := range out.Buckets {
			if b.Name != nil {
				names = append(names, *b.Name)
			}
		}
		if out.ContinuationToken == nil {
			break
		}
		token = out.ContinuationToken
	}
	sort.Strings(names)
	return names, nil
}
