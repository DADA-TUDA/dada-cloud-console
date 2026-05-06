package gitwriter

import (
	"bytes"
	"fmt"
	"text/template"
)

// ServiceDatabaseSpec holds the parameters for rendering a ServiceDatabase CRD manifest.
type ServiceDatabaseSpec struct {
	Name        string
	Namespace   string
	ProjectSlug string
	EnvSlug     string
	Engine      string // e.g. "postgres", "mysql", "redis"
	Version     string
	StorageGB   int
	Replicas    int
}

var serviceDatabaseTemplate = template.Must(template.New("servicedb").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: ServiceDatabase
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
spec:
  engine: {{ .Engine }}
  version: "{{ .Version }}"
  storage:
    sizeGB: {{ .StorageGB }}
  replicas: {{ .Replicas }}
`))

// RenderServiceDatabase generates the YAML manifest for a ServiceDatabase CRD.
func RenderServiceDatabase(spec ServiceDatabaseSpec) (string, error) {
	var buf bytes.Buffer
	if err := serviceDatabaseTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering ServiceDatabase template: %w", err)
	}
	return buf.String(), nil
}
