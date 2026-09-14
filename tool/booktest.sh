#!/usr/bin/env bash
#
# Build this interpreter and run the Crafting Interpreters test suite against
# it, on every parser configuration by default.
#
# The book's tests are not vendored here. They live in munificent/craftinginterpreters,
# which this script shallow-clones into .booktest/ on first run and reuses after.
#
# Usage:
#   tool/booktest.sh                    # rd and llk at k = 1, 2, 3
#   tool/booktest.sh -c rd              # one configuration
#   tool/booktest.sh -f closure         # only test/closure/**
#   tool/booktest.sh -u                 # git pull the corpus first
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
  -u          update the cloned corpus before running
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

if [[ ! -d "$corpus_dir/test" ]]; then
	echo "Cloning the book's tests into ${corpus_dir#"$repo_dir"/} ..."
	git clone --depth 1 "$corpus_url" "$corpus_dir"
elif [[ $update -eq 1 ]]; then
	git -C "$corpus_dir" pull --ff-only
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
	python3 tool/booktest.py -i "$binary" -c "$corpus_dir" -f "$filter" \
		"$@" -- $args || status=1
	echo
done

exit $status
