package drsremote

import (
	"fmt"
	"strings"

	drscommon "github.com/calypr/git-drs/internal/common"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syfoncommon "github.com/calypr/syfon/common"
)

func MatchesScope(obj *drsapi.DrsObject, organization, project string) bool {
	return syfoncommon.DrsObjectMatchesScope(obj, organization, project)
}

func FindMatchingRecord(records []drsapi.DrsObject, organization, projectID string) (*drsapi.DrsObject, error) {
	if len(records) == 0 {
		return nil, nil
	}

	org, project := drscommon.ParseOrgProject(strings.TrimSpace(organization), strings.TrimSpace(projectID))
	if org == "" {
		return nil, fmt.Errorf("could not determine organization from inputs org=%q project=%q", organization, projectID)
	}

	for _, record := range records {
		if record.AccessMethods == nil {
			continue
		}
		for _, access := range *record.AccessMethods {
			authzMap := syfoncommon.AuthzMapFromAccessMethodAuthorizations(access.Authorizations)
			if len(authzMap) == 0 {
				continue
			}
			if syfoncommon.AuthzMapMatchesScope(authzMap, org, project) {
				return &record, nil
			}
		}
	}
	return nil, nil
}
