package drs

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/utils"
)

const DRS_DIR = ".drs"

type DrsWalkFunc func(path string, d *DRSObject) error

func BaseDir() (string, error) {
	gitTopLevel, err := utils.GitTopLevel()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitTopLevel, DRS_DIR), nil
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
	obj := DRSObject{}
	err = json.Unmarshal(data, &obj)
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
