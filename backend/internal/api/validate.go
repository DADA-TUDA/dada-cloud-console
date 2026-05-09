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

// reImage permits registry:port/org/image:tag and mixed-case org names (e.g. ghcr.io/MyOrg/app:v1).
// Rules: starts with alphanumeric; path may contain letters, digits, dots, hyphens, slashes, colons
// (to allow registry:port); must end with :tag where tag is alphanumeric with dots/hyphens; no spaces.
var reImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\-/:]*:[a-zA-Z0-9._\-]+$`)

// ValidateImage checks that an image string is in image:tag format (registry/org/name:tag).
func ValidateImage(image string) error {
	if !reImage.MatchString(image) {
		return fmt.Errorf("image must be in image:tag format (e.g. ghcr.io/org/app:v1.0)")
	}
	return nil
}
