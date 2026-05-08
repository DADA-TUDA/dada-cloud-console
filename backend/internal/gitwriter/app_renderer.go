package gitwriter

import (
	"bytes"
	"fmt"
	"text/template"
)

// AppSpec holds parameters for rendering an App manifest.
type AppSpec struct {
	Name        string
	Namespace   string
	ProjectSlug string
	EnvSlug     string
	Image       string
	Port        int
	Replicas    int
	Profile     string
	OperationID string
}

var appTemplate = template.Must(template.New("app").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: App
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
    dada.io/operation: {{ .OperationID }}
spec:
  project: {{ .ProjectSlug }}
  image: {{ .Image }}
  port: {{ .Port }}
  replicas: {{ .Replicas }}
  profile: {{ .Profile }}
`))

// RenderApp generates the YAML manifest for an App CRD.
func RenderApp(spec AppSpec) (string, error) {
	var buf bytes.Buffer
	if err := appTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering App template: %w", err)
	}
	return buf.String(), nil
}

// AppGitPath returns the canonical Git path for an App manifest.
// Format: clusters/beget-prod/projects/{project}/environments/{env}/apps/{name}/app.yaml
func AppGitPath(projectSlug, envSlug, appName string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/environments/%s/apps/%s/app.yaml",
		projectSlug, envSlug, appName)
}
