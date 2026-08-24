#!/usr/bin/env bash

set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
script_path="$repo_dir/examples/expressions.lox"

# Usage:
#   ./run.sh                         # evaluate the bundled example
#   ./run.sh -print=ast              # print its AST
#   ./run.sh path/to/file.lox        # evaluate another script
#   ./run.sh path/to/file.lox -parser=llk -print=rpn
if [[ $# -gt 0 && "$1" != -* ]]; then
	script_path="$1"
	shift
fi

cd "$repo_dir"

# Let the selected Go binary find its matching toolchain instead of inheriting
# a stale GOROOT from the parent shell.
unset GOROOT

exec go run . "$@" "$script_path"
