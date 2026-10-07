package add

import (
	"fmt"
	"strings"
)

func parseScopeArg(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("organization/project scope is required")
	}

	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid scope %q: expected organization/project", raw)
	}
	organization := strings.TrimSpace(parts[0])
	project := strings.TrimSpace(parts[1])
	if organization == "" || project == "" {
		return "", "", fmt.Errorf("invalid scope %q: expected organization/project", raw)
	}
	return organization, project, nil
}
