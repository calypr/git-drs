package drsremote

import (
	"strings"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func bulkAccessRequest(objects []drsapi.DrsObject) (drsapi.BulkObjectAccessId, bool) {
	req := drsapi.BulkObjectAccessId{}
	items := make([]struct {
		BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
		BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
	}, 0, len(objects))

	for _, obj := range objects {
		objectID := strings.TrimSpace(obj.Id)
		accessID := accessIDForBulkRequest(obj)
		if objectID == "" || accessID == "" {
			continue
		}
		objID := objectID
		accessIDs := []string{accessID}
		items = append(items, struct {
			BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
			BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
		}{
			BulkAccessIds: &accessIDs,
			BulkObjectId:  &objID,
		})
	}
	if len(items) == 0 {
		return req, false
	}
	req.BulkObjectAccessIds = &items
	return req, true
}

func accessIDForBulkRequest(obj drsapi.DrsObject) string {
	if obj.AccessMethods == nil {
		return ""
	}
	for _, method := range *obj.AccessMethods {
		if method.AccessId != nil && strings.TrimSpace(*method.AccessId) != "" {
			return strings.TrimSpace(*method.AccessId)
		}
	}
	return ""
}
