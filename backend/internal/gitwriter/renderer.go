package gitwriter

import (
	"bytes"
	"fmt"
	"text/template"
)

// ServiceDatabaseSpec holds parameters for rendering a ServiceDatabase manifest.
type ServiceDatabaseSpec struct {
	Name            string
	Namespace       string  // k8s namespace: internal-prod
	ProjectSlug     string  // project name slug: internal
	EnvSlug         string  // environment name: prod
	AppRef          string  // app name this database is attached to
	Database        string  // postgresql database name
	BackupEnabled   bool
	BackupSchedule  string // daily, hourly, etc.
	BackupRetention string // 14d, 7d, etc.
	OperationID     string // operation ID for traceability label
}

var serviceDatabaseTemplate = template.Must(template.New("servicedb").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: ServiceDatabase
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
    dada.io/operation: {{ .OperationID }}
spec:
  appRef: {{ .AppRef }}
  engine: postgresql
  database: {{ .Database }}
  backup:
    enabled: {{ .BackupEnabled }}
    frequency: {{ .BackupSchedule }}
    retention: {{ .BackupRetention }}
`))

// RenderServiceDatabase generates the YAML manifest for a ServiceDatabase CRD.
func RenderServiceDatabase(spec ServiceDatabaseSpec) (string, error) {
	var buf bytes.Buffer
	if err := serviceDatabaseTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering ServiceDatabase template: %w", err)
	}
	return buf.String(), nil
}

// ServiceDatabaseGitPath returns the canonical Git path for a ServiceDatabase manifest.
// Format: clusters/beget-prod/projects/{project}/environments/{env}/apps/{appRef}/database.yaml
func ServiceDatabaseGitPath(projectSlug, envSlug, appRef string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/environments/%s/apps/%s/database.yaml",
		projectSlug, envSlug, appRef)
}
