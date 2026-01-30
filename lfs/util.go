package lfs

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/gitrepo"
)

type DrsWalkFunc func(path string, d *drs.DRSObject) error

func BaseDir() (string, error) {
	gitTopLevel, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitTopLevel, common.DRS_DIR), nil
}

type dirWalker struct {
	baseDir  string
	userFunc DrsWalkFunc
}

func (d *dirWalker) call(path string, dir fs.DirEntry, cErr error) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	obj := drs.DRSObject{}
	err = sonic.ConfigFastest.Unmarshal(data, &obj)
	if err != nil {
		return err
	}
	relPath, err := filepath.Rel(d.baseDir, path)
	if err != nil {
		return err
	}
	return d.userFunc(relPath, &obj)
}

func ObjectWalk(f DrsWalkFunc) error {
	baseDir, err := BaseDir()
	if err != nil {
		return err
	}
	ud := dirWalker{baseDir, f}
	return filepath.WalkDir(baseDir, ud.call)
}
