#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
git config core.hooksPath .githooks
printf 'Git hooks enabled: %s\n' "$(git config --get core.hooksPath)"
