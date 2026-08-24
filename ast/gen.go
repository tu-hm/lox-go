package ast

// expr.go and stmt.go are generated from the spec tables in cmd/genast.
// Regenerate with `go generate ./...` after editing those tables; never edit
// the generated files by hand
// (a test in cmd/genast fails if you do).
//
//go:generate go run ../cmd/genast -out .
