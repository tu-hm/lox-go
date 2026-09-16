#!/usr/bin/env bash
#
# Run every .lox file in the corpus through both scanners and require them to
# agree on the sequence of token types.
#
# There are two scanners in this repository now. The Go one builds a slice of
# tokens up front, carries a lexeme and an already-parsed literal on each, and
# reports errors out of band through a package global. The C one hands back a
# token at a time, carries a pointer and a length into the source, and puts its
# errors in the stream as TOKEN_ERROR. They were written months apart, from the
# same book, in different languages, and nothing has ever checked that they read
# the same program the same way.
#
# What is compared is the type sequence only, because that is all both sides can
# be made to print without changing either one's output format. It is enough to
# catch the bugs a scanner actually has: a keyword trie that mis-munches
# `orchid`, a two-character operator that grabs one character too many or too
# few, a comment that eats a newline, a number that swallows a trailing dot.
#
# Usage:
#   tool/scandiff.sh                # the whole corpus
#   tool/scandiff.sh scanning       # only test/scanning/**
#   tool/scandiff.sh -v             # also list what was skipped and why

set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cache_dir="$repo_dir/.booktest"
glox="$cache_dir/glox"
clox="$repo_dir/clox/build/debug-libc/clox"

verbose=0
if [[ "${1:-}" == "-v" ]]; then
	verbose=1
	shift
fi
filter="${1:-}"

cd "$repo_dir"

mkdir -p "$cache_dir"
make -C clox >/dev/null
# Let the selected Go binary find its matching toolchain instead of inheriting
# a stale GOROOT from the parent shell.
(unset GOROOT; go build -o "$glox" .)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

compared=0
skipped=0
failed=0
declare -a skips=()

while IFS= read -r file; do
	[[ -n "$filter" && "$file" != test/"$filter"* ]] && continue

	"$clox" "$file" > "$work/c.txt"

	# An error token has no counterpart on the Go side, which reports to stderr
	# and emits nothing. A lexeme with escaped whitespace in it is a multi-line
	# string, and the Go token's String() would spill it across several lines of
	# output, so its first column would stop being a token type.
	if grep -q '^ *[0-9|]\{1,\} *ERROR ' "$work/c.txt"; then
		skips+=("$file (scanner error)")
		skipped=$((skipped + 1))
		continue
	fi
	if grep -q "'.*\\\\[nrt]" "$work/c.txt"; then
		skips+=("$file (multi-line lexeme)")
		skipped=$((skipped + 1))
		continue
	fi

	"$glox" -tokens "$file" > "$work/g.txt" 2> "$work/g.err" || true

	# The resolver's unused-variable warnings are ours, not the book's, and are
	# not scanner output. Anything else on stderr means the Go scanner reported
	# an error and its token slice has a hole where clox has a TOKEN_ERROR.
	if grep -qv '^\[line [0-9]\{1,\}\] Warning' "$work/g.err" 2>/dev/null; then
		skips+=("$file (go scanner error)")
		skipped=$((skipped + 1))
		continue
	fi

	awk '{print $2}' "$work/c.txt" > "$work/c.types"
	awk '{print $1}' "$work/g.txt" > "$work/g.types"

	compared=$((compared + 1))
	if ! diff -q "$work/c.types" "$work/g.types" >/dev/null; then
		failed=$((failed + 1))
		echo "FAIL $file"
		diff -u "$work/g.types" "$work/c.types" \
			| sed -e '1,2d' -e 's/^-/  go   /' -e 's/^+/  clox /' -e 's/^ /       /' \
			| head -20
	fi
done < <(find test -name '*.lox' -not -path '*/benchmark/*' | sort)

if (( verbose && skipped > 0 )); then
	echo "Skipped:"
	printf '  %s\n' "${skips[@]}"
fi

echo "$compared compared, $failed disagreed, $skipped skipped"
(( failed == 0 ))
