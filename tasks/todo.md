# Build on GitHub

- [x] Reproduce the current GitHub build surface and identify the missing piece
- [x] Add a GitHub Actions workflow that matches the release build path
- [x] Verify backend, frontend, Helm render, and Docker image build steps locally as far as the environment allows
- [x] Confirm the workflow file is present and ready for GitHub to pick up

## Review

Added a GitHub Actions workflow that mirrors the release build path from Jenkins and uploads the releaseable backend/frontend artifacts.
