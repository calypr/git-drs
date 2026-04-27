package gitrepo

import (
	"fmt"
	"strings"
)

type ResolvedBucketScope struct {
	Bucket string
	Prefix string
}

// ResolveBucketScope returns the effective bucket/prefix for an org+project.
// It prefers explicit bucket-map entries and validates conflicts with any
// configured fallback values.
func ResolveBucketScope(organization, project, configuredBucket, configuredPrefix string) (ResolvedBucketScope, error) {
	organization = strings.TrimSpace(organization)
	project = strings.TrimSpace(project)
	configuredBucket = strings.TrimSpace(configuredBucket)
	configuredPrefix = strings.Trim(strings.TrimSpace(configuredPrefix), "/")

	if organization == "" {
		if configuredBucket == "" {
			return ResolvedBucketScope{}, fmt.Errorf("bucket is required when organization is empty")
		}
		return ResolvedBucketScope{
			Bucket: configuredBucket,
			Prefix: configuredPrefix,
		}, nil
	}

	orgMapping, hasOrgMapping, err := GetBucketMapping(organization, "")
	if err != nil {
		return ResolvedBucketScope{}, fmt.Errorf("resolve bucket mapping for organization=%q: %w", organization, err)
	}

	var projectMapping BucketMapping
	hasProjectMapping := false
	if project != "" {
		projectMapping, hasProjectMapping, err = getExactBucketMapping(organization, project)
		if err != nil {
			return ResolvedBucketScope{}, fmt.Errorf("resolve bucket mapping for organization=%q project=%q: %w", organization, project, err)
		}
	}

	if hasOrgMapping {
		bucket := strings.TrimSpace(orgMapping.Bucket)
		if bucket == "" {
			return ResolvedBucketScope{}, fmt.Errorf("bucket mapping for organization=%q is missing bucket", organization)
		}
		return ResolvedBucketScope{
			Bucket: bucket,
			Prefix: joinBucketPrefixes(orgMapping.Prefix, projectMapping.Prefix),
		}, nil
	}

	if hasProjectMapping {
		bucket := strings.TrimSpace(projectMapping.Bucket)
		if bucket == "" {
			return ResolvedBucketScope{}, fmt.Errorf("bucket mapping for organization=%q project=%q is missing bucket", organization, project)
		}
		return ResolvedBucketScope{
			Bucket: bucket,
			Prefix: strings.Trim(strings.TrimSpace(projectMapping.Prefix), "/"),
		}, nil
	}

	if configuredBucket == "" {
		return ResolvedBucketScope{}, fmt.Errorf("bucket is required (or configure mapping with `git drs bucket add-organization --organization %s --path <scheme>://<bucket>/<prefix>`)", organization)
	}
	return ResolvedBucketScope{
		Bucket: configuredBucket,
		Prefix: configuredPrefix,
	}, nil
}

func getExactBucketMapping(org, project string) (BucketMapping, bool, error) {
	org = strings.TrimSpace(org)
	project = strings.TrimSpace(project)
	if org == "" || project == "" {
		return BucketMapping{}, false, nil
	}
	bucket, err := GetGitConfigString(projectBucketKey(org, project, "bucket"))
	if err != nil {
		return BucketMapping{}, false, err
	}
	if strings.TrimSpace(bucket) == "" {
		return BucketMapping{}, false, nil
	}
	prefix, err := GetGitConfigString(projectBucketKey(org, project, "prefix"))
	if err != nil {
		return BucketMapping{}, false, err
	}
	return BucketMapping{
		Bucket: strings.TrimSpace(bucket),
		Prefix: strings.Trim(strings.TrimSpace(prefix), "/"),
	}, true, nil
}

func joinBucketPrefixes(parts ...string) string {
	joined := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), "/")
		if part != "" {
			joined = append(joined, part)
		}
	}
	return strings.Join(joined, "/")
}
