package gitrepo

import (
	"fmt"
	"regexp"
	"strings"
)

type BucketMapping struct {
	Bucket string
	Prefix string
}

var bucketMapKeyPartSanitizer = regexp.MustCompile(`[^a-z0-9_-]+`)

func normalizeBucketMapKeyPart(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, ".", "_")
	v = bucketMapKeyPartSanitizer.ReplaceAllString(v, "_")
	v = strings.Trim(v, "_")
	return v
}

func orgBucketKey(org, field string) string {
	return fmt.Sprintf("drs.bucketmap.orgs.%s.%s", normalizeBucketMapKeyPart(org), field)
}

func projectBucketKey(org, project, field string) string {
	return fmt.Sprintf("drs.bucketmap.projects.%s.%s.%s", normalizeBucketMapKeyPart(org), normalizeBucketMapKeyPart(project), field)
}

func SetBucketMapping(org, project, bucket, prefix string) error {
	org = strings.TrimSpace(org)
	project = strings.TrimSpace(project)
	bucket = strings.TrimSpace(bucket)
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if org == "" {
		return fmt.Errorf("organization is required")
	}
	if bucket == "" {
		return fmt.Errorf("bucket is required")
	}

	configs := map[string]string{}
	if project != "" {
		configs[projectBucketKey(org, project, "bucket")] = bucket
		if prefix != "" {
			configs[projectBucketKey(org, project, "prefix")] = prefix
		}
	} else {
		configs[orgBucketKey(org, "bucket")] = bucket
		if prefix != "" {
			configs[orgBucketKey(org, "prefix")] = prefix
		}
	}
	return SetGitConfigOptions(configs)
}

func GetBucketMapping(org, project string) (BucketMapping, bool, error) {
	org = strings.TrimSpace(org)
	project = strings.TrimSpace(project)
	if org == "" {
		return BucketMapping{}, false, nil
	}

	// Project-specific mapping takes precedence.
	if project != "" {
		bucket, err := GetGitConfigString(projectBucketKey(org, project, "bucket"))
		if err != nil {
			return BucketMapping{}, false, err
		}
		if strings.TrimSpace(bucket) != "" {
			prefix, err := GetGitConfigString(projectBucketKey(org, project, "prefix"))
			if err != nil {
				return BucketMapping{}, false, err
			}
			return BucketMapping{
				Bucket: strings.TrimSpace(bucket),
				Prefix: strings.Trim(strings.TrimSpace(prefix), "/"),
			}, true, nil
		}
	}

	// Fallback: organization-level mapping.
	bucket, err := GetGitConfigString(orgBucketKey(org, "bucket"))
	if err != nil {
		return BucketMapping{}, false, err
	}
	if strings.TrimSpace(bucket) == "" {
		return BucketMapping{}, false, nil
	}
	prefix, err := GetGitConfigString(orgBucketKey(org, "prefix"))
	if err != nil {
		return BucketMapping{}, false, err
	}
	return BucketMapping{
		Bucket: strings.TrimSpace(bucket),
		Prefix: strings.Trim(strings.TrimSpace(prefix), "/"),
	}, true, nil
}
