#!/usr/bin/env bash
# init-state-repo.sh — Creates a local non-bare git repo at /tmp/dada-state-repo
# for development use as the GitOps state repository.
set -euo pipefail

REPO_PATH="${DADA_STATE_REPO:-/tmp/dada-state-repo}"

if [ -d "$REPO_PATH/.git" ]; then
  echo "State repo already exists at $REPO_PATH — skipping init."
  exit 0
fi

echo "Initialising GitOps state repo at $REPO_PATH ..."
mkdir -p "$REPO_PATH"
git -C "$REPO_PATH" init
git -C "$REPO_PATH" config user.name "DADA Platform Bot"
git -C "$REPO_PATH" config user.email "bot@dada-tuda.ru"

# Create initial directory structure and commit
mkdir -p "$REPO_PATH/clusters/local/namespaces"
cat > "$REPO_PATH/clusters/local/namespaces/.gitkeep" <<'EOF'
EOF

git -C "$REPO_PATH" add .
git -C "$REPO_PATH" commit -m "chore: initial state repo scaffold"

echo "Done. State repo ready at $REPO_PATH"
