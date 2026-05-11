package gitwriter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dada-tuda/console/backend/internal/gitwriter"
)

func publicapiGoldenPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "golden", "publicapi", name)
}

func readPublicapiGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(publicapiGoldenPath(name))
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(b)
}

func TestRenderPublicApi_Basic(t *testing.T) {
	spec := gitwriter.PublicApiSpec{
		Name:           "api-myservice-ru",
		Namespace:      "client-a-prod",
		ProjectSlug:    "client-a",
		EnvSlug:        "prod",
		ServiceName:    "profi-backend",
		ServicePort:    3000,
		FQDN:           "api.myservice.ru",
		LBTarget:       "93.189.231.60",
		AuthEnabled:    true,
		AuthScheme:     "platform-jwt",
		AuthScopes:     []string{"api.read", "api.write"},
		SwaggerEnabled: true,
		SwaggerPath:    "/v3/api-docs",
		SwaggerTitle:   "My Service API",
		OperationID:    "op-test-1234",
	}
	got, err := gitwriter.RenderPublicApi(spec)
	if err != nil {
		t.Fatalf("RenderPublicApi: %v", err)
	}
	want := readPublicapiGolden(t, "basic.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match basic.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderPublicApi_NoAuthNoSwagger(t *testing.T) {
	spec := gitwriter.PublicApiSpec{
		Name:           "app-internal-ru",
		Namespace:      "internal-prod",
		ProjectSlug:    "internal",
		EnvSlug:        "prod",
		ServiceName:    "codex-lb",
		ServicePort:    8080,
		FQDN:           "app.internal.ru",
		LBTarget:       "93.189.231.60",
		AuthEnabled:    false,
		AuthScheme:     "none",
		AuthScopes:     nil,
		SwaggerEnabled: false,
		SwaggerPath:    "/v3/api-docs",
		SwaggerTitle:   "codex-lb",
		OperationID:    "op-test-5678",
	}
	got, err := gitwriter.RenderPublicApi(spec)
	if err != nil {
		t.Fatalf("RenderPublicApi: %v", err)
	}
	want := readPublicapiGolden(t, "no-auth-no-swagger.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match no-auth-no-swagger.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestPublicApiGitPath(t *testing.T) {
	cases := []struct {
		project, env, app, name, want string
	}{
		{"client-a", "prod", "profi-backend", "api-myservice-ru",
			"clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/publicapi-api-myservice-ru.yaml"},
		{"internal", "prod", "codex-lb", "app-internal-ru",
			"clusters/beget-prod/projects/internal/environments/prod/apps/codex-lb/publicapi-app-internal-ru.yaml"},
	}
	for _, tc := range cases {
		got := gitwriter.PublicApiGitPath(tc.project, tc.env, tc.app, tc.name)
		if got != tc.want {
			t.Errorf("PublicApiGitPath(%q,%q,%q,%q) = %q, want %q",
				tc.project, tc.env, tc.app, tc.name, got, tc.want)
		}
	}
}

func TestFQDNToName(t *testing.T) {
	cases := []struct{ fqdn, want string }{
		{"api.myservice.ru", "api-myservice-ru"},
		{"app.internal.ru", "app-internal-ru"},
		{"payments.dada-tuda.ru", "payments-dada-tuda-ru"},
	}
	for _, tc := range cases {
		got := gitwriter.FQDNToName(tc.fqdn)
		if got != tc.want {
			t.Errorf("FQDNToName(%q) = %q, want %q", tc.fqdn, got, tc.want)
		}
	}
}
