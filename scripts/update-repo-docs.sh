#!/usr/bin/env bash

set -euo pipefail

if [[ -n "${DEBUG:-}" ]]; then
    set -x
fi

repo='wodby/docs'
branch='master'
root_dir="${PWD}"
out_dir="${root_dir}/out/docs/cli-reference"
clone_dir="${root_dir}/out/repo_docs"

if [[ -z "${MACHINE_USER_API_TOKEN:-}" ]]; then
    echo 'ERROR: MACHINE_USER_API_TOKEN is required' 1>&2
    exit 1
fi

bash "${root_dir}/scripts/cli-docs-build.sh" "${out_dir}"

rm -rf "${clone_dir}"
git clone https://x-access-token:"${MACHINE_USER_API_TOKEN}"@github.com/"${repo}".git "${clone_dir}"

cd "${clone_dir}"
git config --replace-all remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
git fetch origin "${branch}:refs/remotes/origin/${branch}" --tags || true
git checkout -B "${branch}" "origin/${branch}"
git config "branch.${branch}.remote" origin
git config "branch.${branch}.merge" "refs/heads/${branch}"

docs_dir="${clone_dir}/2.0/docs/dev/cli-reference"
rm -rf "${docs_dir}"
mkdir -p "${docs_dir}"
cp -R "${out_dir}/." "${docs_dir}/"

python3 - "${clone_dir}/2.0/mkdocs.yml" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
content = path.read_text()
nav_needle = "      - CLI: dev/cli.md\n"
nav_insert = "      - CLI reference: dev/cli-reference/index.md\n"
not_in_nav_entry = "  dev/cli-reference/wodby*.md\n"
changed = False

if nav_insert not in content:
    if nav_needle not in content:
        raise SystemExit("Could not find Development CLI nav entry")
    content = content.replace(nav_needle, nav_needle + nav_insert, 1)
    changed = True

if not_in_nav_entry in content:
    content = content.replace(not_in_nav_entry, "", 1)
    changed = True

if changed:
    path.write_text(content)
PY

git add 2.0/docs/dev/cli-reference 2.0/mkdocs.yml
git update-index -q --refresh

if git diff --cached --quiet; then
    echo 'Nothing to commit'
    exit 0
fi

if [[ -n "${MACHINE_USER_EMAIL:-}" ]]; then
    git config user.email "${MACHINE_USER_EMAIL}"
fi
if [[ -n "${MACHINE_USER:-}" ]]; then
    git config user.name "${MACHINE_USER}"
fi

commit_args=()
if [[ -n "${MACHINE_USER:-}" && -n "${MACHINE_USER_EMAIL:-}" ]]; then
    commit_args+=("--author=${MACHINE_USER} <${MACHINE_USER_EMAIL}>")
fi

short_sha="$(git -C "${root_dir}" rev-parse --short "${GITHUB_SHA:-HEAD}")"
git commit "${commit_args[@]}" -m "Update CLI docs ${short_sha}"
git push origin "${branch}"
