package drsmap

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
)

func CreateLfsPointer(drsObj *drs.DRSObject, dst string) error {
	sumMap := hash.ConvertHashInfoToMap(drsObj.Checksums)
	if len(sumMap) == 0 {
		return fmt.Errorf("no checksums found for DRS object")
	}

	// find sha256 checksum
	var shaSum string
	for csType, cs := range sumMap {
		if csType == hash.ChecksumTypeSHA256.String() {
			shaSum = cs
			break
		}
	}
	if shaSum == "" {
		return fmt.Errorf("no sha256 checksum found for DRS object")
	}

	// create pointer file content
	pointerContent := "version https://git-lfs.github.com/spec/v1\n"
	pointerContent += fmt.Sprintf("oid sha256:%s\n", shaSum)
	pointerContent += fmt.Sprintf("size %d\n", drsObj.Size)

	// write to file
	err := os.WriteFile(dst, []byte(pointerContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write LFS pointer file: %w", err)
	}

	return nil
}
