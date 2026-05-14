package renderer

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ProjectSpec holds parameters for a Project manifest.
type ProjectSpec struct {
	Project            string         `yaml:"project"`
	DisplayName        string         `yaml:"displayName"`
	OwnerType          string         `yaml:"ownerType,omitempty"`
	DefaultEnvironment string         `yaml:"defaultEnvironment,omitempty"`
	Quotas             map[string]any `yaml:"quotas"`
}

func RenderProject(spec ProjectSpec) (string, error) {
	if spec.OwnerType == "" {
		spec.OwnerType = "team"
	}
	if spec.DefaultEnvironment == "" {
		spec.DefaultEnvironment = "prod"
	}
	if spec.Quotas == nil {
		spec.Quotas = map[string]any{}
	}

	b, err := yaml.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("rendering Project: %w", err)
	}
	return string(b), nil
}

func ProjectGitPath(projectSlug string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/project.yaml", projectSlug)
}
