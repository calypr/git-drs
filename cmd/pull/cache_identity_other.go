//go:build !darwin && !linux

package pull

import "os"

func cacheFileIdentity(os.FileInfo) (uint64, uint64) { return 0, 0 }
