#!/usr/bin/env bash
set -e

REPO_PATH="${GIT_STATE_REPO_PATH:-/tmp/dada-state-repo}"

if [ -d "$REPO_PATH/.git" ]; then
    echo "Git state repo already exists at $REPO_PATH"
    exit 0
fi

echo "Initializing GitOps state repo at $REPO_PATH"
mkdir -p "$REPO_PATH"
git init "$REPO_PATH"
cd "$REPO_PATH"
git config user.email "bot@dada-tuda.ru"
git config user.name "DADA Platform Bot"

# Create initial structure
mkdir -p clusters/beget-prod/projects/internal/environments/dev
mkdir -p clusters/beget-prod/projects/internal/environments/prod
mkdir -p clusters/beget-prod/projects/client-a/environments/prod

cat > clusters/beget-prod/projects/internal/project.yaml << 'EOF'
# DADA Platform State — managed by DADA Cloud Console
project: internal
displayName: DADA Internal
EOF

git add -A
git commit -m "init: initial GitOps state repository structure"
echo "Git state repo initialized at $REPO_PATH"
