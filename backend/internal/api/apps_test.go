package api_test

import (
	"testing"

	"github.com/dada-tuda/console/backend/internal/api"
)

func TestValidateImage(t *testing.T) {
	good := []string{
		"ghcr.io/dada-tuda/codex-lb:1.14.2",
		"registry.dada-tuda.ru/app:latest",
		"nginx:1.25",
		"my-app:v2.3.1-rc1",
		"ghcr.io/MyOrg/my-app:v1.0",          // uppercase org (GitHub Container Registry)
		"registry.example.com:5000/app:v1.0",  // registry with port
		"MYAPP:latest",                         // uppercase image name
	}
	bad := []string{
		"",
		"no-tag",
		"has space:v1",
		"image with spaces:v1",
	}
	for _, img := range good {
		if err := api.ValidateImage(img); err != nil {
			t.Errorf("expected %q to be valid, got: %v", img, err)
		}
	}
	for _, img := range bad {
		if err := api.ValidateImage(img); err == nil {
			t.Errorf("expected %q to be invalid", img)
		}
	}
}
