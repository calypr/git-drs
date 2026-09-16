package filter

import (
	"os"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
)

func ShouldSkipSmudge() bool {
	if val := os.Getenv("GIT_LFS_SKIP_SMUDGE"); val != "" {
		return val == "1" || strings.ToLower(val) == "true"
	}
	if val := os.Getenv("GIT_DRS_SKIP_SMUDGE"); val != "" {
		return val == "1" || strings.ToLower(val) == "true"
	}
	if valStr, err := gitrepo.GetGitConfigString("drs.skipsmudge"); err == nil && valStr != "" {
		if val, err := strconv.ParseBool(valStr); err == nil {
			return val
		}
	}
	if valStr, err := gitrepo.GetGitConfigString("lfs.skipsmudge"); err == nil && valStr != "" {
		if val, err := strconv.ParseBool(valStr); err == nil {
			return val
		}
	}
	return true
}
