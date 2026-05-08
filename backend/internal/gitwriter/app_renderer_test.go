package gitwriter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dada-tuda/console/backend/internal/gitwriter"
)

func appGoldenPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "golden", "app", name)
}

func readAppGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(appGoldenPath(name))
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(b)
}

func TestRenderApp_Basic(t *testing.T) {
	spec := gitwriter.AppSpec{
		Name:        "codex-lb",
		Namespace:   "internal-prod",
		ProjectSlug: "internal",
		EnvSlug:     "prod",
		Image:       "ghcr.io/dada-tuda/codex-lb:1.14.2",
		Port:        8080,
		Replicas:    2,
		Profile:     "small",
		OperationID: "op-test-1234",
	}
	got, err := gitwriter.RenderApp(spec)
	if err != nil {
		t.Fatalf("RenderApp: %v", err)
	}
	want := readAppGolden(t, "basic.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file basic.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderApp_CustomProfile(t *testing.T) {
	spec := gitwriter.AppSpec{
		Name:        "profi-backend",
		Namespace:   "client-a-prod",
		ProjectSlug: "client-a",
		EnvSlug:     "prod",
		Image:       "registry.dada-tuda.ru/profi-backend:2.3.1",
		Port:        3000,
		Replicas:    3,
		Profile:     "medium",
		OperationID: "op-test-5678",
	}
	got, err := gitwriter.RenderApp(spec)
	if err != nil {
		t.Fatalf("RenderApp: %v", err)
	}
	want := readAppGolden(t, "custom-profile.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file custom-profile.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestAppGitPath(t *testing.T) {
	cases := []struct {
		project, env, app, want string
	}{
		{"internal", "prod", "codex-lb",
			"clusters/beget-prod/projects/internal/environments/prod/apps/codex-lb/app.yaml"},
		{"client-a", "prod", "profi-backend",
			"clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/app.yaml"},
	}
	for _, tc := range cases {
		got := gitwriter.AppGitPath(tc.project, tc.env, tc.app)
		if got != tc.want {
			t.Errorf("AppGitPath(%q,%q,%q) = %q, want %q",
				tc.project, tc.env, tc.app, got, tc.want)
		}
	}
}
