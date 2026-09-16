#!/usr/bin/env bash
#
# Compile every expression in tool/expressions.txt with clox and render the same
# expression with the Go AST printer, and require the two to agree.
#
# The two sides have nothing in common except the book. clox parses with a Pratt
# parser -- one table of binding powers and one loop -- and emits bytecode. The
# Go front end parses with a ladder of eleven recursive-descent functions
# (parser/parser.go) and builds a tree, which ast.RPNPrinter then walks. They
# were written months apart, in different languages, and until now there was no
# program both could read.
#
# What makes the comparison possible is that bytecode for an expression *is* its
# postfix traversal. Operands are pushed before the operator that consumes them,
# which is the definition of RPN, so
#
#     OP_CONSTANT 1, OP_CONSTANT 2, OP_ADD, OP_CONSTANT 3, OP_MULTIPLY
#
# and
#
#     1 2 + 3 *
#
# are the same sentence. Parentheses leave no trace in either one: clox's
# grouping() emits nothing, and the Go printer's VisitGroupingExpr drops them.
#
# That makes this a test of exactly the two things a new precedence parser gets
# wrong -- precedence and associativity -- because those are the only things that
# change the operand order.
#
# What it cannot compare: anything that is not arithmetic on number literals,
# since chapter 17's clox has no other expressions; and numbers with more than
# six significant digits, because the C disassembler prints constants with %g.
# The expression list says so too.
#
# Usage:
#   tool/rpndiff.sh        # every expression
#   tool/rpndiff.sh -v     # print each comparison, not just the failures

set -euo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cache_dir="$repo_dir/.booktest"
glox="$cache_dir/glox"
clox="$repo_dir/clox/build/debug-libc/clox"

verbose=0
[[ "${1:-}" == "-v" ]] && verbose=1

cd "$repo_dir"

mkdir -p "$cache_dir"
make -C clox >/dev/null
# Let the selected Go binary find its matching toolchain instead of inheriting
# a stale GOROOT from the parent shell.
(unset GOROOT; go build -o "$glox" .)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

compared=0
failed=0

while IFS= read -r expr; do
	# Blank lines and comments.
	[[ -z "${expr// /}" || "$expr" == \#* ]] && continue

	printf '%s' "$expr" > "$work/c.lox"
	# The Go front end parses a program, so the expression needs to be a
	# statement. clox's parses one expression and then requires end of input, so
	# it must not be.
	printf '%s;' "$expr" > "$work/g.lox"

	# OP_CONSTANT and OP_CONSTANT_LONG carry the value in the fifth field, in
	# single quotes. Everything else is an operator, except OP_RETURN, which is
	# the instruction that prints the result rather than part of the expression.
	c_rpn="$("$clox" -dump "$work/c.lox" | awk -v q="'" '
		$3 == "OP_CONSTANT" || $3 == "OP_CONSTANT_LONG" {
			v = $5; gsub(q, "", v); out = out sep v; sep = " "; next
		}
		$3 == "OP_ADD"      { out = out sep "+";      sep = " "; next }
		$3 == "OP_SUBTRACT" { out = out sep "-";      sep = " "; next }
		$3 == "OP_MULTIPLY" { out = out sep "*";      sep = " "; next }
		$3 == "OP_DIVIDE"   { out = out sep "/";      sep = " "; next }
		$3 == "OP_NEGATE"   { out = out sep "negate"; sep = " "; next }
		END { print out }
	')"

	# The Go printer wraps an expression statement as "(expr <rpn>)".
	g_rpn="$("$glox" -print=rpn "$work/g.lox")"
	g_rpn="${g_rpn#(expr }"
	g_rpn="${g_rpn%)}"

	compared=$((compared + 1))
	if [[ "$c_rpn" != "$g_rpn" ]]; then
		failed=$((failed + 1))
		echo "FAIL $expr"
		echo "  go   $g_rpn"
		echo "  clox $c_rpn"
	elif (( verbose )); then
		printf '%-22s %s\n' "$expr" "$c_rpn"
	fi
done < tool/expressions.txt

echo "$compared compared, $failed disagreed"
(( failed == 0 ))
