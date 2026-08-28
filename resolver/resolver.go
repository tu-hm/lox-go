// Package resolver is the static pass between parsing and execution. It walks
// the tree once and answers, for every variable use, which declaration that use
// refers to — expressed as the number of scopes between the two. The
// interpreter then reads the variable at that depth instead of searching the
// environment chain, which makes variable lookup both faster and, more
// importantly, correct: the answer is fixed by the program text, so a closure
// cannot be changed later by a declaration that appears after it.
//
// The pass also reports what it learns along the way: the binding rules the
// grammar cannot express, as static errors, and locals nobody reads, as
// warnings.
package resolver

import (
	"compiler101/ast"
	"compiler101/interpreter"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Resolver is one pass over one tree. It is not reusable: the scope stack and
// the collected diagnostics belong to a single program, which is why the REPL
// builds a new one per line while keeping the same interpreter.
type Resolver struct {
	interpreter *interpreter.Interpreter
	// scopes is a stack of block scopes, innermost last. The global scope is
	// deliberately absent: globals stay dynamic, so a script may call a
	// function declared further down and the REPL may define names one line
	// at a time.
	scopes          []*scope
	currentFunction functionType
	currentClass    classType
	errs            []error
	warnings        []*errors.Warning
}

// The compiler checks the two visitor interfaces here, so adding a node type to
// package ast breaks this file rather than silently skipping the new node.
var (
	_ ast.ExprVisitor = (*Resolver)(nil)
	_ ast.StmtVisitor = (*Resolver)(nil)
)

// functionType records what kind of body the pass is currently inside. It
// started as "is there a function at all", for "Can't return from top-level
// code."; a method is a function for that purpose, and an initializer is a
// function that may not return a value.
type functionType int

const (
	functionNone functionType = iota
	functionFunction
	functionMethod
	functionInitializer
	// functionClassMethod is a method reached through the class. `this` is
	// legal in one — it is the class — but nothing about it is an initializer,
	// whatever it is named.
	functionClassMethod
)

// classType is the same question one level up, and exists for exactly one
// diagnostic: `this` outside any class. A bare currentClass flag would do
// today, but chapter 13 adds a third state for a class with a superclass.
type classType int

const (
	classNone classType = iota
	classClass
)

// bindingKind is what a name was declared as. It only affects diagnostics: how
// to describe the name, and whether it is exempt from the unused check.
type bindingKind int

const (
	bindingVariable bindingKind = iota
	bindingFunction
	bindingClass
	bindingParameter
	// bindingThis is the implicit receiver. It occupies a real slot like any
	// other binding, because the interpreter addresses it like any other
	// binding — but the source never wrote it, so it is exempt from the unused
	// check. Without that exemption every class whose methods ignore `this`
	// would warn.
	bindingThis
)

func (k bindingKind) String() string {
	switch k {
	case bindingFunction:
		return "function"
	case bindingClass:
		return "class"
	case bindingParameter:
		return "parameter"
	default:
		return "variable"
	}
}

// binding is everything the pass knows about one declared name.
type binding struct {
	name token.Token
	kind bindingKind
	// slot is this name's index within its own scope, numbered in declaration
	// order. Together with the scope distance it tells the interpreter exactly
	// where the value sits, so the runtime never compares a name.
	slot int
	// defined reports whether the initializer has finished. Declared-but-not-
	// defined is exactly what makes "var a = a;" detectable.
	defined bool
	// read reports whether any expression reads the value. Assigning to a
	// variable is not reading it, which is what lets the unused check catch a
	// local that is only ever written.
	read bool
}

// scope is one block's worth of declarations, kept both by name for lookup and
// in declaration order so diagnostics come out in source order.
type scope struct {
	byName map[string]*binding
	order  []*binding
}

// useKind distinguishes the two ways a variable can be mentioned. Only a read
// counts against the unused check.
type useKind int

const (
	useRead useKind = iota
	useWrite
)

// Resolve binds every local variable use in statements into interp. It returns
// the static errors it found — a non-empty slice means the program must not be
// executed — and the warnings, which do not. Both are also reported to the user
// as they are found, the way the parser reports syntax errors.
func Resolve(interp *interpreter.Interpreter, statements []ast.Stmt) ([]error, []*errors.Warning) {
	r := &Resolver{interpreter: interp}
	r.resolveStatements(statements)
	return r.errs, r.warnings
}

func (r *Resolver) resolveStatements(statements []ast.Stmt) {
	for _, statement := range statements {
		r.resolveStmt(statement)
	}
}

func (r *Resolver) resolveStmt(stmt ast.Stmt) { stmt.Accept(r) }

func (r *Resolver) resolveExpr(e ast.Expr) { e.Accept(r) }

func (r *Resolver) beginScope() {
	r.scopes = append(r.scopes, &scope{byName: make(map[string]*binding)})
}

// endScope is where the unused check happens: a name still unread when its
// scope closes can never be read, because nothing outside the scope can name
// it. That is the whole reason this is a static question at all.
func (r *Resolver) endScope() {
	closing := r.innermost()
	r.scopes = r.scopes[:len(r.scopes)-1]

	for _, b := range closing.order {
		// A parameter is exempt: its presence is dictated by the caller's
		// signature rather than by the body, and Lox has no way to spell
		// "deliberately unused". Go draws the line in the same place. `this` is
		// exempt because the source never declared it at all.
		if b.kind == bindingParameter || b.kind == bindingThis || b.read {
			continue
		}
		r.warn(b.name, "Local "+b.kind.String()+" '"+b.name.Lexeme+"' is never used.")
	}
}

func (r *Resolver) innermost() *scope { return r.scopes[len(r.scopes)-1] }

// declare adds name to the innermost scope, marked unfinished. Splitting
// declaration from definition is what separates the two shadowing cases: "var a
// = a;" refers to the variable being declared and is an error, while the same
// text in a nested scope refers to the outer one and is fine.
func (r *Resolver) declare(name token.Token, kind bindingKind) {
	if len(r.scopes) == 0 {
		return // global scope, where Lox deliberately allows redeclaration
	}
	current := r.innermost()
	if _, declared := current.byName[name.Lexeme]; declared {
		// The first declaration keeps the scope slot. A second one would make
		// every later diagnostic about this scope report the name twice.
		r.fail(name, "Already a variable with this name in this scope.")
		return
	}
	b := &binding{name: name, kind: kind, slot: len(current.order)}
	current.byName[name.Lexeme] = b
	current.order = append(current.order, b)
}

// define marks name's initializer finished, making the variable readable.
func (r *Resolver) define(name token.Token) {
	if len(r.scopes) == 0 {
		return
	}
	if b, declared := r.innermost().byName[name.Lexeme]; declared {
		b.defined = true
	}
}

// resolveLocal walks the scope stack innermost-first and hands the interpreter
// the hop count. Not finding the name is not an error: that is what "global"
// means here, and the interpreter falls back to a dynamic lookup which may
// still fail at runtime.
func (r *Resolver) resolveLocal(e ast.Expr, name token.Token, use useKind) {
	for depth := len(r.scopes) - 1; depth >= 0; depth-- {
		b, declared := r.scopes[depth].byName[name.Lexeme]
		if !declared {
			continue
		}
		if use == useRead {
			b.read = true
		}
		r.interpreter.Resolve(e, len(r.scopes)-1-depth, b.slot)
		return
	}
}

func (r *Resolver) fail(t token.Token, message string) {
	r.errs = append(r.errs, errors.ResolveErrorAt(t, message))
}

func (r *Resolver) warn(t token.Token, message string) {
	r.warnings = append(r.warnings, errors.WarnToken(t, message))
}

func (r *Resolver) VisitBlockStmt(stmt *ast.Block) any {
	r.beginScope()
	r.resolveStatements(stmt.Statements)
	r.endScope()
	return nil
}

func (r *Resolver) VisitVarStmt(stmt *ast.Var) any {
	r.declare(stmt.Name, bindingVariable)
	if stmt.Initializer != nil {
		r.resolveExpr(stmt.Initializer)
	}
	r.define(stmt.Name)
	return nil
}

// VisitClassStmt opens one scope per class, holding nothing but `this`.
//
// That scope is the pass's half of a bargain with the runtime: it is what
// LoxFunction.bind creates, and `this` is the sole binding in it, so `this`
// lives at slot 0 and a method body finds it one hop out. Declaring it here
// rather than special-casing it in the interpreter is what lets `this` be read
// through the same slot-addressed path as every other local — this
// implementation's local environments hold no names to look up.
//
// Method names are deliberately not declared. A method is reached through its
// class's method map, never through a scope, so there is no binding to make and
// no unused-method warning to give.
func (r *Resolver) VisitClassStmt(stmt *ast.Class) any {
	enclosing := r.currentClass
	r.currentClass = classClass
	defer func() { r.currentClass = enclosing }()

	r.declare(stmt.Name, bindingClass)
	r.define(stmt.Name)

	r.beginScope()
	this := token.Token{Type: token.THIS, Lexeme: "this", Line: stmt.Name.Line}
	r.declare(this, bindingThis)
	r.define(this)

	for _, method := range stmt.Methods {
		kind := functionMethod
		if method.Name.Lexeme == "init" {
			// An initializer has to stay callable: construction calls it with
			// the class's arguments, and a getter cannot be called at all.
			if method.IsGetter {
				r.fail(method.Name, "Can't declare an initializer as a getter.")
			}
			kind = functionInitializer
		}
		r.resolveFunction(method, kind)
	}
	// Class methods share the same scope, which is what makes `this` mean the
	// class inside one: bind puts the class in the slot this scope reserved,
	// so the distance a class method body counts is the distance it gets.
	for _, method := range stmt.ClassMethods {
		r.resolveFunction(method, functionClassMethod)
	}
	r.endScope()
	return nil
}

// VisitFunctionStmt defines the function's own name before resolving its body,
// so the body can refer to the function and recurse.
func (r *Resolver) VisitFunctionStmt(stmt *ast.Function) any {
	r.declare(stmt.Name, bindingFunction)
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
		r.declare(parameter, bindingParameter)
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
	// A bare `return;` stays legal in an initializer: it is an early exit, and
	// the runtime still hands back the instance. Only naming a value is an
	// error, because construction has no other answer to give.
	if r.currentFunction == functionInitializer && stmt.Value != nil {
		r.fail(stmt.Keyword, "Can't return a value from an initializer.")
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
		if b, declared := r.innermost().byName[e.Name.Lexeme]; declared && !b.defined {
			r.fail(e.Name, "Can't read local variable in its own initializer.")
		}
	}
	r.resolveLocal(e, e.Name, useRead)
	return nil
}

// VisitAssignExpr resolves the assigned value first, then the target. An
// assignment is a variable use like any other as far as binding goes, but it
// writes rather than reads, so it does not make the variable "used".
func (r *Resolver) VisitAssignExpr(e *ast.Assign) any {
	r.resolveExpr(e.Value)
	r.resolveLocal(e, e.Name, useWrite)
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

// VisitGetExpr and VisitSetExpr resolve the object and stop. A property name is
// not a variable: it names a slot on an object that may not exist until the
// line runs, so there is nothing static to bind it to. That is the whole reason
// property access stays a dynamic lookup while variable access does not.
func (r *Resolver) VisitGetExpr(e *ast.Get) any {
	r.resolveExpr(e.Object)
	return nil
}

func (r *Resolver) VisitSetExpr(e *ast.Set) any {
	r.resolveExpr(e.Value)
	r.resolveExpr(e.Object)
	return nil
}

// VisitThisExpr resolves `this` as the local variable the class scope declared.
// Outside a class there is no such scope, and resolving anyway would quietly
// turn it into a global lookup that fails much later with a worse message.
func (r *Resolver) VisitThisExpr(e *ast.This) any {
	if r.currentClass == classNone {
		r.fail(e.Keyword, "Can't use 'this' outside of a class.")
		return nil
	}
	r.resolveLocal(e, e.Keyword, useRead)
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
