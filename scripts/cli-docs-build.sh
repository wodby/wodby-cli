#!/usr/bin/env bash

set -euo pipefail

if [[ -n "${DEBUG:-}" ]]; then
    set -x
fi

out_dir="${1:-./out/docs/cli-reference}"

go run ./cmd/wodby-docs --dir "${out_dir}"
