package utils

import (
	"fmt"
	"strings"
)

const (
	DRS_DIR = ".drs"
)

func ProjectToResource(project string) (string, error) {
	if !strings.Contains(project, "-") {
		return "", fmt.Errorf("error: invalid project ID %s in config file, ID should look like <program>-<project>", project)
	}
	projectIdArr := strings.SplitN(project, "-", 2)
	return "/programs/" + projectIdArr[0] + "/projects/" + projectIdArr[1], nil
}
