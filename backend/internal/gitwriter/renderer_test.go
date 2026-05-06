package gitwriter_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dada-tuda/console/backend/internal/gitwriter"
)

func goldenPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "golden", "servicedatabase", name)
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("reading golden file %s: %v", name, err)
	}
	return string(b)
}

func TestRenderServiceDatabase_Basic(t *testing.T) {
	spec := gitwriter.ServiceDatabaseSpec{
		Name:            "codex-lb-db",
		Namespace:       "internal-prod",
		ProjectSlug:     "internal",
		EnvSlug:         "prod",
		AppRef:          "codex-lb",
		Database:        "codexlb",
		BackupEnabled:   false,
		BackupSchedule:  "daily",
		BackupRetention: "14d",
		OperationID:     "op-test-1234",
	}

	got, err := gitwriter.RenderServiceDatabase(spec)
	if err != nil {
		t.Fatalf("RenderServiceDatabase: %v", err)
	}

	want := readGolden(t, "basic.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file basic.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderServiceDatabase_WithBackup(t *testing.T) {
	spec := gitwriter.ServiceDatabaseSpec{
		Name:            "profi-db",
		Namespace:       "client-a-prod",
		ProjectSlug:     "client-a",
		EnvSlug:         "prod",
		AppRef:          "profi-backend",
		Database:        "profi",
		BackupEnabled:   true,
		BackupSchedule:  "daily",
		BackupRetention: "7d",
		OperationID:     "op-test-5678",
	}

	got, err := gitwriter.RenderServiceDatabase(spec)
	if err != nil {
		t.Fatalf("RenderServiceDatabase: %v", err)
	}

	want := readGolden(t, "with-backup.yaml")
	if got != want {
		t.Errorf("rendered YAML does not match golden file with-backup.yaml\n\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestServiceDatabaseGitPath(t *testing.T) {
	cases := []struct {
		project, env, app, want string
	}{
		{"internal", "prod", "codex-lb",
			"clusters/beget-prod/projects/internal/environments/prod/apps/codex-lb/database.yaml"},
		{"client-a", "prod", "profi-backend",
			"clusters/beget-prod/projects/client-a/environments/prod/apps/profi-backend/database.yaml"},
	}
	for _, tc := range cases {
		got := gitwriter.ServiceDatabaseGitPath(tc.project, tc.env, tc.app)
		if got != tc.want {
			t.Errorf("ServiceDatabaseGitPath(%q,%q,%q) = %q, want %q",
				tc.project, tc.env, tc.app, got, tc.want)
		}
	}
}
