package lookup

import (
	"fmt"
	"strings"

	drsapi "github.com/calypr/syfon/apigen/drs"
	syfoncommon "github.com/calypr/syfon/client/access"
)

func ParseOrgProject(org, project string) (string, string) {
	if org != "" {
		return org, project
	}
	if project == "" {
		return "", ""
	}
	if !strings.Contains(project, "-") {
		return "default", project
	}
	parts := strings.SplitN(project, "-", 2)
	return parts[0], parts[1]
}

func MatchesScope(obj *drsapi.DrsObject, organization, project string) bool {
	if obj == nil || obj.ControlledAccess == nil {
		return false
	}
	authzMap := syfoncommon.ControlledAccessToAuthzMap(*obj.ControlledAccess)
	organization = strings.TrimSpace(organization)
	project = strings.TrimSpace(project)
	if organization == "" {
		return false
	}
	projects, ok := authzMap[organization]
	if !ok {
		return false
	}
	if len(projects) == 0 {
		return true
	}
	for _, candidate := range projects {
		if strings.TrimSpace(candidate) == project {
			return true
		}
	}
	return false
}

func FindMatchingRecord(records []drsapi.DrsObject, organization, projectID string) (*drsapi.DrsObject, error) {
	if len(records) == 0 {
		return nil, nil
	}

	org, project := ParseOrgProject(strings.TrimSpace(organization), strings.TrimSpace(projectID))
	if org == "" {
		return nil, fmt.Errorf("could not determine organization from inputs org=%q project=%q", organization, projectID)
	}

	for _, record := range records {
		if MatchesScope(&record, org, project) {
			return &record, nil
		}
	}
	return nil, nil
}
