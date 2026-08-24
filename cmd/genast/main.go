// Command genast writes the AST node types for package ast.
//
// Four node types is little enough to hand-write, and chapter 5 does exactly
// that first. The generator earns itself back from chapter 8 onwards: a
// parallel Stmt hierarchy arrives, and by chapter 13 there are roughly twenty
// node types, each needing a struct, an Accept, a marker method and a visitor
// interface entry. Hand-editing that after every chapter is how you end up with
// a visitor that silently doesn't compile.
//
// Usage:
//
//	go run ./cmd/genast -out ./ast
//
// or, from package ast, `go generate ./...`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type field struct{ Name, Type string }

type node struct {
	Name   string
	Doc    string
	Fields []field
}

// hierarchy is one generated file: a base interface, its visitor, and its
// nodes. Chapter 8's Stmt tree is a second value of this type plus one more
// call to generate.
type hierarchy struct {
	Base  string // "Expr" — the interface and file name
	Noun  string // "expression" — for doc comments
	Nodes []node
}

// exprTypes is the spec. The book passes these as strings
// ("Binary : Expr left, Token operator, Expr right") because Java has no
// concise literal syntax; Go does, so this is a struct table instead. The
// compiler type-checks it, the editor autocompletes it, and there is no
// malformed-spec error path to write.
var exprs = hierarchy{
	Base: "Expr",
	Noun: "expression",
	Nodes: []node{
		{
			Name: "Assign",
			Doc:  "Assign stores Value in the variable named by Name and evaluates to Value.",
			Fields: []field{
				{"Name", "token.Token"},
				{"Value", "Expr"},
			},
		},
		{
			Name: "Binary",
			Doc:  "Binary is an infix operation: Left Operator Right.",
			Fields: []field{
				{"Left", "Expr"},
				{"Operator", "token.Token"},
				{"Right", "Expr"},
			},
		},
		{
			Name:   "Grouping",
			Doc:    "Grouping is a parenthesised expression. It survives into the tree because\nit changes precedence, and the interpreter must not re-associate across it.",
			Fields: []field{{"Expression", "Expr"}},
		},
		{
			Name:   "Literal",
			Doc:    "Literal is a bare value: a number, a string, a boolean, or Lox nil.\nValue is nil exactly when the source said nil.",
			Fields: []field{{"Value", "any"}},
		},
		{
			Name: "Logical",
			Doc:  "Logical is a short-circuiting and/or operation: Left Operator Right.",
			Fields: []field{
				{"Left", "Expr"},
				{"Operator", "token.Token"},
				{"Right", "Expr"},
			},
		},
		{
			Name: "Unary",
			Doc:  "Unary is a prefix operation: Operator Right.",
			Fields: []field{
				{"Operator", "token.Token"},
				{"Right", "Expr"},
			},
		},
		{
			Name:   "Variable",
			Doc:    "Variable reads the binding identified by Name.",
			Fields: []field{{"Name", "token.Token"}},
		},
	},
}

var stmts = hierarchy{
	Base: "Stmt",
	Noun: "statement",
	Nodes: []node{
		{
			Name:   "Block",
			Doc:    "Block executes Statements in a nested lexical scope.",
			Fields: []field{{"Statements", "[]Stmt"}},
		},
		{
			Name:   "Expression",
			Doc:    "Expression evaluates an expression for its side effects.",
			Fields: []field{{"Expression", "Expr"}},
		},
		{
			Name: "If",
			Doc:  "If executes ThenBranch when Condition is truthy, otherwise ElseBranch when present.",
			Fields: []field{
				{"Condition", "Expr"},
				{"ThenBranch", "Stmt"},
				{"ElseBranch", "Stmt"},
			},
		},
		{
			Name:   "Print",
			Doc:    "Print evaluates Expression and writes its Lox representation.",
			Fields: []field{{"Expression", "Expr"}},
		},
		{
			Name: "Var",
			Doc:  "Var declares Name, initialized by Initializer or nil when it is absent.",
			Fields: []field{
				{"Name", "token.Token"},
				{"Initializer", "Expr"},
			},
		},
		{
			Name: "While",
			Doc:  "While repeatedly executes Body while Condition remains truthy.",
			Fields: []field{
				{"Condition", "Expr"},
				{"Body", "Stmt"},
			},
		},
	},
}

func main() {
	out := flag.String("out", ".", "output directory")
	flag.Parse()

	if err := generate(*out, exprs); err != nil {
		log.Fatal(err)
	}
	if err := generate(*out, stmts); err != nil {
		log.Fatal(err)
	}
}

// generate renders one hierarchy to <dir>/<base>.go, lowercased.
func generate(dir string, h hierarchy) error {
	src, err := render(h)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, strings.ToLower(h.Base)+".go"), src, 0o644)
}

// render is generate without the write, so the golden test can compare bytes
// without touching the filesystem.
func render(h hierarchy) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, h); err != nil {
		return nil, fmt.Errorf("executing template for %s: %w", h.Base, err)
	}

	// gofmt the output. This lets the template ignore indentation entirely and
	// fails loudly when the template emits invalid Go — printing the
	// unformatted buffer, because otherwise you are debugging blind.
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated %s: %w\n%s", h.Base, err, buf.String())
	}
	return src, nil
}

// doc turns a possibly-multiline Doc string into Go comment lines.
var funcs = template.FuncMap{
	"doc": func(s string) string {
		if s == "" {
			return ""
		}
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lines[i] = "// " + l
		}
		return strings.Join(lines, "\n")
	},
}

// The "Code generated by ... DO NOT EDIT." first line is the exact prefix
// recognised by gofmt, linters and GitHub diffs.
//
// The token import is emitted unconditionally. Both current hierarchies use it:
// expression variables and assignments carry names, as does the Var statement.
// If a future hierarchy does not, format.Source will flag the unused import.
var tmpl = template.Must(template.New("ast").Funcs(funcs).Parse(`
// Code generated by cmd/genast. DO NOT EDIT.

package ast

import "compiler101/lexer/token"

// {{.Base}} is the sealed set of {{.Noun}} nodes. is{{.Base}} is unexported, so
// no type outside this package can join the set.
type {{.Base}} interface {
	Accept(v {{.Base}}Visitor) any
	is{{.Base}}()
}

// {{.Base}}Visitor is one operation over the tree. Adding a node type below
// breaks every visitor that does not handle it — at compile time, which is the
// whole reason for the pattern.
type {{.Base}}Visitor interface {
{{- range .Nodes}}
	Visit{{.Name}}{{$.Base}}(e *{{.Name}}) any
{{- end}}
}
{{range .Nodes}}
{{with .Doc}}{{. | doc}}{{end}}
type {{.Name}} struct {
{{- range .Fields}}
	{{.Name}} {{.Type}}
{{- end}}
}

func (e *{{.Name}}) Accept(v {{$.Base}}Visitor) any { return v.Visit{{.Name}}{{$.Base}}(e) }
func (e *{{.Name}}) is{{$.Base}}() {}
{{end}}`))
