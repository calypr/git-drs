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

	mapping, ok, err := GetBucketMapping(organization, project)
	if err != nil {
		return ResolvedBucketScope{}, fmt.Errorf("resolve bucket mapping for organization=%q project=%q: %w", organization, project, err)
	}

	if !ok {
		if configuredBucket == "" {
			return ResolvedBucketScope{}, fmt.Errorf("bucket is required (or configure mapping with `git drs bucket add --organization %s --project %s --bucket <name> [--path ...]`)", organization, project)
		}
		return ResolvedBucketScope{
			Bucket: configuredBucket,
			Prefix: configuredPrefix,
		}, nil
	}

	mappedBucket := strings.TrimSpace(mapping.Bucket)
	mappedPrefix := strings.Trim(strings.TrimSpace(mapping.Prefix), "/")
	if mappedBucket == "" {
		return ResolvedBucketScope{}, fmt.Errorf("bucket mapping for organization=%q project=%q is missing bucket", organization, project)
	}

	// Mapping is authoritative when present.
	return ResolvedBucketScope{
		Bucket: mappedBucket,
		Prefix: mappedPrefix,
	}, nil
}
