package gitwriter

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// PublicApiSpec holds parameters for rendering a PublicApi manifest.
type PublicApiSpec struct {
	Name        string
	Namespace   string
	ProjectSlug string
	EnvSlug     string
	ServiceName string
	ServicePort int
	FQDN        string
	LBTarget    string
	AuthEnabled    bool
	AuthScheme     string
	AuthScopes     []string
	SwaggerEnabled bool
	SwaggerPath    string
	SwaggerTitle   string
	OperationID string
}

var publicApiTemplate = template.Must(template.New("publicapi").Parse(`apiVersion: platform.dada-tuda.ru/v1alpha1
kind: PublicApi
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    dada.io/project: {{ .ProjectSlug }}
    dada.io/environment: {{ .EnvSlug }}
    dada.io/operation: {{ .OperationID }}
spec:
  upstream:
    serviceName: {{ .ServiceName }}
    servicePort: {{ .ServicePort }}
  route:
    prefix: /
    pathPattern: /**
    stripPrefix: false
  auth:
    enabled: {{ .AuthEnabled }}
    scheme: {{ .AuthScheme }}{{ if and .AuthEnabled .AuthScopes }}
    scopes:{{ range .AuthScopes }}
      - {{ . }}{{ end }}{{ end }}
  swagger:
    enabled: {{ .SwaggerEnabled }}
    path: {{ .SwaggerPath }}
    title: {{ .SwaggerTitle }}
  dns:
    enabled: true
    fqdn: {{ .FQDN }}
    recordType: A
    target: {{ .LBTarget }}
`))

// RenderPublicApi generates the YAML manifest for a PublicApi CRD.
func RenderPublicApi(spec PublicApiSpec) (string, error) {
	var buf bytes.Buffer
	if err := publicApiTemplate.Execute(&buf, spec); err != nil {
		return "", fmt.Errorf("rendering PublicApi template: %w", err)
	}
	return buf.String(), nil
}

// PublicApiGitPath returns the canonical Git path for a PublicApi manifest.
func PublicApiGitPath(projectSlug, envSlug, appName, publicApiName string) string {
	return fmt.Sprintf("clusters/beget-prod/projects/%s/environments/%s/apps/%s/publicapi-%s.yaml",
		projectSlug, envSlug, appName, publicApiName)
}

// FQDNToName converts a FQDN to a valid Kubernetes resource name.
func FQDNToName(fqdn string) string {
	return strings.ReplaceAll(fqdn, ".", "-")
}
