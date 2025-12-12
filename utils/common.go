package utils

import (
	"fmt"
	"strings"
)

const (
	DRS_DIR = ".drs"
)

func ProjectToResource(project string) (string, error) {
	if !strings.Contains(project, "-") {
		return "", fmt.Errorf("error: invalid project ID %s in config file, ID should look like <program>-<project>", project)
	}
	projectIdArr := strings.SplitN(project, "-", 2)
	return "/programs/" + projectIdArr[0] + "/projects/" + projectIdArr[1], nil
}

// AddUnique appends items from 'toAdd' to 'existing' only if they're not already present.
// Returns the updated slice with unique items.
func AddUnique[T comparable](existing []T, toAdd []T) []T {
	seen := make(map[T]bool, len(existing))
	for _, item := range existing {
		seen[item] = true
	}

	for _, item := range toAdd {
		if !seen[item] {
			existing = append(existing, item)
			seen[item] = true
		}
	}
	return existing
}
