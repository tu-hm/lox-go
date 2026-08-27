// Package resolver is the static pass between parsing and execution. It walks
// the tree once and answers, for every variable use, which declaration that use
// refers to — expressed as the number of scopes between the two. The
// interpreter then reads the variable at that depth instead of searching the
// environment chain, which makes variable lookup both faster and, more
// importantly, correct: the answer is fixed by the program text, so a closure
// cannot be changed later by a declaration that appears after it.
//
// The pass also reports the binding rules the grammar cannot express, such as
// "return" outside a function. Those are static errors: nothing has run yet, so
// the program is refused rather than interrupted.
package resolver

import (
	"compiler101/ast"
	"compiler101/interpreter"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Resolver is one pass over one tree. It is not reusable: the scope stack and
// the collected errors belong to a single program, which is why the REPL builds
// a new one per line while keeping the same interpreter.
type Resolver struct {
	interpreter *interpreter.Interpreter
	// scopes is a stack of block scopes, innermost last. A name maps to
	// whether its initializer has finished — declared-but-unfinished is
	// exactly what makes "var a = a;" detectable. The global scope is
	// deliberately absent: globals stay dynamic, so a script may call a
	// function declared further down and the REPL may define names one line
	// at a time.
	scopes          []map[string]bool
	currentFunction functionType
	errs            []error
}

// The compiler checks the two visitor interfaces here, so adding a node type to
// package ast breaks this file rather than silently skipping the new node.
var (
	_ ast.ExprVisitor = (*Resolver)(nil)
	_ ast.StmtVisitor = (*Resolver)(nil)
)

// functionType records whether the pass is currently inside a function body,
// which is all "Can't return from top-level code." needs to know. Chapter 12
// adds methods and initializers to this set.
type functionType int

const (
	functionNone functionType = iota
	functionFunction
)

// Resolve binds every local variable use in statements into interp and returns
// the static errors it found. A non-empty result means the program must not be
// executed. Errors are also reported to the user as they are found, the same
// way the parser reports syntax errors.
func Resolve(interp *interpreter.Interpreter, statements []ast.Stmt) []error {
	r := &Resolver{interpreter: interp}
	r.resolveStatements(statements)
	return r.errs
}

func (r *Resolver) resolveStatements(statements []ast.Stmt) {
	for _, statement := range statements {
		r.resolveStmt(statement)
	}
}

func (r *Resolver) resolveStmt(stmt ast.Stmt) { stmt.Accept(r) }

func (r *Resolver) resolveExpr(e ast.Expr) { e.Accept(r) }

func (r *Resolver) beginScope() {
	r.scopes = append(r.scopes, make(map[string]bool))
}

func (r *Resolver) endScope() {
	r.scopes = r.scopes[:len(r.scopes)-1]
}

func (r *Resolver) innermost() map[string]bool { return r.scopes[len(r.scopes)-1] }

// declare adds name to the innermost scope, marked unfinished. Splitting
// declaration from definition is what separates the two shadowing cases: "var a
// = a;" refers to the variable being declared and is an error, while the same
// text in a nested scope refers to the outer one and is fine.
func (r *Resolver) declare(name token.Token) {
	if len(r.scopes) == 0 {
		return // global scope, where Lox deliberately allows redeclaration
	}
	scope := r.innermost()
	if _, declared := scope[name.Lexeme]; declared {
		r.fail(name, "Already a variable with this name in this scope.")
	}
	scope[name.Lexeme] = false
}

// define marks name's initializer finished, making the variable readable.
func (r *Resolver) define(name token.Token) {
	if len(r.scopes) == 0 {
		return
	}
	r.innermost()[name.Lexeme] = true
}

// resolveLocal walks the scope stack innermost-first and hands the interpreter
// the hop count. Not finding the name is not an error: that is what "global"
// means here, and the interpreter falls back to a dynamic lookup which may
// still fail at runtime.
func (r *Resolver) resolveLocal(e ast.Expr, name token.Token) {
	for scope := len(r.scopes) - 1; scope >= 0; scope-- {
		if _, declared := r.scopes[scope][name.Lexeme]; declared {
			r.interpreter.Resolve(e, len(r.scopes)-1-scope)
			return
		}
	}
}

func (r *Resolver) fail(t token.Token, message string) {
	r.errs = append(r.errs, errors.ResolveErrorAt(t, message))
}

func (r *Resolver) VisitBlockStmt(stmt *ast.Block) any {
	r.beginScope()
	r.resolveStatements(stmt.Statements)
	r.endScope()
	return nil
}

func (r *Resolver) VisitVarStmt(stmt *ast.Var) any {
	r.declare(stmt.Name)
	if stmt.Initializer != nil {
		r.resolveExpr(stmt.Initializer)
	}
	r.define(stmt.Name)
	return nil
}

// VisitFunctionStmt defines the function's own name before resolving its body,
// so the body can refer to the function and recurse.
func (r *Resolver) VisitFunctionStmt(stmt *ast.Function) any {
	r.declare(stmt.Name)
	r.define(stmt.Name)
	r.resolveFunction(stmt, functionFunction)
	return nil
}

// resolveFunction opens exactly one scope for the parameters and the body
// together, mirroring the runtime: LoxFunction.Call binds parameters into the
// environment the body then executes in. Every beginScope here must correspond
// to one NewEnvironment there, or every distance inside is off by one.
func (r *Resolver) resolveFunction(stmt *ast.Function, kind functionType) {
	enclosing := r.currentFunction
	r.currentFunction = kind
	defer func() { r.currentFunction = enclosing }()

	r.beginScope()
	for _, parameter := range stmt.Params {
		r.declare(parameter)
		r.define(parameter)
	}
	r.resolveStatements(stmt.Body)
	r.endScope()
}

func (r *Resolver) VisitExpressionStmt(stmt *ast.Expression) any {
	r.resolveExpr(stmt.Expression)
	return nil
}

// VisitIfStmt resolves both branches. Static analysis has no values to test and
// is not trying to predict which branch runs; it visits every path exactly once.
func (r *Resolver) VisitIfStmt(stmt *ast.If) any {
	r.resolveExpr(stmt.Condition)
	r.resolveStmt(stmt.ThenBranch)
	if stmt.ElseBranch != nil {
		r.resolveStmt(stmt.ElseBranch)
	}
	return nil
}

func (r *Resolver) VisitPrintStmt(stmt *ast.Print) any {
	r.resolveExpr(stmt.Expression)
	return nil
}

func (r *Resolver) VisitReturnStmt(stmt *ast.Return) any {
	if r.currentFunction == functionNone {
		r.fail(stmt.Keyword, "Can't return from top-level code.")
	}
	if stmt.Value != nil {
		r.resolveExpr(stmt.Value)
	}
	return nil
}

// VisitWhileStmt resolves the body once, not once per iteration: scope
// structure is a property of the syntax, not of how often it executes.
func (r *Resolver) VisitWhileStmt(stmt *ast.While) any {
	r.resolveExpr(stmt.Condition)
	r.resolveStmt(stmt.Body)
	return nil
}

// VisitVariableExpr is where the initializer rule is enforced: a name found in
// the innermost scope but not yet defined can only be the variable whose
// initializer is being resolved right now.
func (r *Resolver) VisitVariableExpr(e *ast.Variable) any {
	if len(r.scopes) > 0 {
		if defined, declared := r.innermost()[e.Name.Lexeme]; declared && !defined {
			r.fail(e.Name, "Can't read local variable in its own initializer.")
		}
	}
	r.resolveLocal(e, e.Name)
	return nil
}

// VisitAssignExpr resolves the assigned value first, then the target. An
// assignment is a variable use like any other, so it gets its own entry in the
// interpreter's table.
func (r *Resolver) VisitAssignExpr(e *ast.Assign) any {
	r.resolveExpr(e.Value)
	r.resolveLocal(e, e.Name)
	return nil
}

func (r *Resolver) VisitBinaryExpr(e *ast.Binary) any {
	r.resolveExpr(e.Left)
	r.resolveExpr(e.Right)
	return nil
}

func (r *Resolver) VisitCallExpr(e *ast.Call) any {
	r.resolveExpr(e.Callee)
	for _, argument := range e.Arguments {
		r.resolveExpr(argument)
	}
	return nil
}

func (r *Resolver) VisitGroupingExpr(e *ast.Grouping) any {
	r.resolveExpr(e.Expression)
	return nil
}

// VisitLiteralExpr has no subexpressions and mentions no variable, so there is
// nothing to resolve.
func (r *Resolver) VisitLiteralExpr(*ast.Literal) any { return nil }

// VisitLogicalExpr resolves both operands. Short-circuiting decides what runs,
// which is a runtime question; both operands still exist in the text.
func (r *Resolver) VisitLogicalExpr(e *ast.Logical) any {
	r.resolveExpr(e.Left)
	r.resolveExpr(e.Right)
	return nil
}

func (r *Resolver) VisitUnaryExpr(e *ast.Unary) any {
	r.resolveExpr(e.Right)
	return nil
}
