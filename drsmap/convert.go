package drsmap

import (
	"github.com/calypr/git-drs/drs"
)

func DrsObjectToLfsRecord(obj *drs.DRSObject) *LfsFileInfo {
	return &LfsFileInfo{
		Name:       obj.Name,
		Size:       obj.Size,
		Checkout:   false,
		Downloaded: false,
		OidType:    string(obj.Checksums[0].Type),
		Oid:        obj.Checksums[0].Checksum,
		Version:    "https://git-lfs.github.com/spec/v1",
	}
}
