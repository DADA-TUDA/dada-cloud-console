package api

import (
	"fmt"
	"regexp"
)

var (
	reKubeName = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,61}[a-z0-9]$|^[a-z0-9]$`)
	rePgName   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

func validateKubeName(name string) error {
	if !reKubeName.MatchString(name) {
		return fmt.Errorf("name must be lowercase alphanumeric with hyphens, max 63 chars")
	}
	return nil
}

func validatePgName(name string) error {
	if !rePgName.MatchString(name) {
		return fmt.Errorf("database name must start with lowercase letter, alphanumeric+underscore, max 63 chars")
	}
	return nil
}
