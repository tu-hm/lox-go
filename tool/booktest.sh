#!/usr/bin/env bash
#
# Build this interpreter and run the Crafting Interpreters test suite against
# it, on every parser configuration by default.
#
# The tests are vendored in test/ — see test/README.md for the pinned upstream
# commit. -u ignores them and runs a fresh clone of upstream instead, which is
# how to find out whether the vendored copy has fallen behind.
#
# Usage:
#   tool/booktest.sh                    # rd and llk at k = 1, 2, 3
#   tool/booktest.sh -c rd              # one configuration
#   tool/booktest.sh -f closure         # only test/closure/**
#   tool/booktest.sh -u                 # against a fresh upstream clone
#
# Anything after -- goes to tool/booktest.py, e.g. --strict-stderr.

set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cache_dir="$repo_dir/.booktest"
corpus_dir="$cache_dir/craftinginterpreters"
binary="$cache_dir/glox"
corpus_url="https://github.com/munificent/craftinginterpreters.git"

usage() {
	cat <<'EOF'
Build this interpreter and run the Crafting Interpreters test suite against it.

  -c CONFIG   all (default), rd, llk, llk1, llk2, llk3
  -f PREFIX   only run tests under test/PREFIX
  -u          ignore the vendored test/ and run a fresh upstream clone
  -h          this message

Anything after -- is passed to tool/booktest.py: --strict-stderr to judge our
unused-variable warnings, --run-skipped to also run what the jlox suite skips.
EOF
}

config=all
filter=""
update=0
while getopts ":c:f:uh" opt; do
	case "$opt" in
	c) config="$OPTARG" ;;
	f) filter="$OPTARG" ;;
	u) update=1 ;;
	h)
		usage
		exit 0
		;;
	*)
		echo "unknown option -$OPTARG" >&2
		exit 64
		;;
	esac
done
shift $((OPTIND - 1))

mkdir -p "$cache_dir"

# The vendored corpus sits at test/, so the repo itself is a corpus root: the
# runner looks for <corpus>/test. -u trades it for a fresh clone of upstream.
corpus="$repo_dir"
if [[ $update -eq 1 || ! -d "$repo_dir/test" ]]; then
	if [[ ! -d "$corpus_dir/.git" ]]; then
		echo "Cloning upstream into ${corpus_dir#"$repo_dir"/} ..."
		git clone --depth 1 "$corpus_url" "$corpus_dir"
	else
		git -C "$corpus_dir" pull --ff-only
	fi
	corpus="$corpus_dir"
	echo "Running against upstream $(git -C "$corpus_dir" rev-parse --short HEAD), not the vendored test/."
fi

cd "$repo_dir"

# Let the selected Go binary find its matching toolchain instead of inheriting
# a stale GOROOT from the parent shell. Same reason as run.sh.
unset GOROOT
go build -o "$binary" .

case "$config" in
all) configs=("" "-parser=llk -k=1" "-parser=llk -k=2" "-parser=llk -k=3") ;;
rd) configs=("") ;;
llk) configs=("-parser=llk") ;;
llk1) configs=("-parser=llk -k=1") ;;
llk2) configs=("-parser=llk -k=2") ;;
llk3) configs=("-parser=llk -k=3") ;;
*)
	echo "unknown configuration '$config': want all, rd, llk, llk1, llk2 or llk3" >&2
	exit 64
	;;
esac

status=0
for args in "${configs[@]}"; do
	echo "=== ${args:-default (recursive descent)} ==="
	# shellcheck disable=SC2086 # $args is a flag list and must word-split.
	python3 tool/booktest.py -i "$binary" -c "$corpus" -f "$filter" \
		"$@" -- $args || status=1
	echo
done

exit $status
