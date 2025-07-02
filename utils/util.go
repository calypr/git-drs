package utils

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitTopLevel() (string, error) {
	path, err := SimpleRun([]string{"git", "rev-parse", "--show-toplevel"})
	path = strings.TrimSuffix(path, "\n")
	return path, err
}

func SimpleRun(cmds []string) (string, error) {
	exePath, err := exec.LookPath(cmds[0])
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	cmd := exec.Command(exePath, cmds[1:]...)
	cmd.Stdout = buf
	err = cmd.Run()
	return buf.String(), err
}

func DrsTopLevel() (string, error) {
	base, err := GitTopLevel()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DRS_DIR), nil
}
