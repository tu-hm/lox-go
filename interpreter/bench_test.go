package interpreter_test

import (
	"io"
	"testing"

	"compiler101/ast"
	"compiler101/interpreter"
	"compiler101/lexer"
	"compiler101/parser"
	"compiler101/resolver"
)

// benchProgram parses and resolves once, outside the measured loop: these
// benchmarks are about executing a resolved tree, not about producing one.
func benchProgram(b *testing.B, source string) ([]ast.Stmt, *interpreter.Interpreter) {
	b.Helper()

	statements, parseErrs := parser.New(lexer.Lex(source)).ParseProgram()
	if len(parseErrs) != 0 {
		b.Fatalf("parse: %v", parseErrs)
	}
	interp := interpreter.NewWithWriter(io.Discard)
	if errs, _ := resolver.Resolve(interp, statements); len(errs) != 0 {
		b.Fatalf("resolve: %v", errs)
	}
	return statements, interp
}

// BenchmarkFib is call-heavy: most of the work is creating a call environment
// and binding one parameter into it.
func BenchmarkFib(b *testing.B) {
	program, interp := benchProgram(b, `
		fun fib(n) {
			if (n < 2) return n;
			return fib(n - 1) + fib(n - 2);
		}
		fib(18);
	`)

	for b.Loop() {
		if err := interp.Execute(program); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

// BenchmarkLocalAccess is read-and-write heavy inside one scope, with the
// variables two and three environments up from the innermost block.
func BenchmarkLocalAccess(b *testing.B) {
	program, interp := benchProgram(b, `
		fun sum() {
			var total = 0;
			var i = 0;
			while (i < 500) {
				{
					total = total + i;
					i = i + 1;
				}
			}
			return total;
		}
		sum();
	`)

	for b.Loop() {
		if err := interp.Execute(program); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}
