// Package interpreter evaluates expressions and executes Lox statement trees.
package interpreter

import (
	"fmt"
	"io"
	"os"

	"compiler101/ast"
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

type Interpreter struct {
	globals     *Environment
	environment *Environment
	// locals is what the resolver hands over: for each variable use, where
	// the declaration it refers to lives.
	//
	// The key is the syntax node itself. Every ast node is used through a
	// pointer and Go compares pointer keys by identity, which is the same
	// guarantee the book gets from Java's IdentityHashMap — two textually
	// identical uses in different places stay distinct entries. Nothing may
	// copy a node by value or rebuild the tree between resolution and
	// execution, or the entry recorded here becomes unreachable.
	locals map[ast.Expr]slot
	out    io.Writer
}

// slot is where a resolved variable lives: distance environments up the chain,
// at index within that environment. Both numbers come from the syntax, so
// neither costs anything to find at runtime.
type slot struct{ distance, index int }

var (
	_ ast.ExprVisitor = (*Interpreter)(nil)
	_ ast.StmtVisitor = (*Interpreter)(nil)
)

type runtimeSignal struct {
	err *errors.RuntimeError
}

func New() *Interpreter {
	return NewWithWriter(os.Stdout)
}

// NewWithWriter creates an interpreter whose Lox print statements write to
// out. Supplying the writer keeps language output deterministic in tests.
func NewWithWriter(out io.Writer) *Interpreter {
	if out == nil {
		out = io.Discard
	}
	globals := NewEnvironment(nil)
	globals.Define("clock", nativeClock{})
	return &Interpreter{
		globals:     globals,
		environment: globals,
		locals:      make(map[ast.Expr]slot),
		out:         out,
	}
}

func (i *Interpreter) Interpret(e ast.Expr) (value any, err error) {
	defer recoverRuntime(&err)

	return i.evaluate(e), nil
}

// Execute runs a complete program. Statements do not produce a value; output
// is an explicit side effect of a Lox print statement.
func (i *Interpreter) Execute(statements []ast.Stmt) (err error) {
	defer recoverRuntime(&err)
	for _, statement := range statements {
		i.execute(statement)
	}
	return nil
}

func recoverRuntime(err *error) {
	r := recover()
	if r == nil {
		return
	}
	sig, ok := r.(runtimeSignal)
	if !ok {
		panic(r)
	}
	*err = sig.err
}

func (i *Interpreter) evaluate(e ast.Expr) any {
	return e.Accept(i)
}

func (i *Interpreter) execute(stmt ast.Stmt) {
	stmt.Accept(i)
}

// Resolve records where one variable use finds its declaration: distance scopes
// out, at index within that scope. Package resolver owns the analysis; the
// interpreter only stores the answer and trusts it. Executing a tree that was
// never resolved is not an error but a different language: every local would be
// looked up in globals.
func (i *Interpreter) Resolve(e ast.Expr, distance, index int) {
	i.locals[e] = slot{distance: distance, index: index}
}

// lookUpVariable reads a variable use through whichever mechanism applies. A
// resolved use hops a known number of environments; an unresolved one is by
// definition global, and only that case can still fail at runtime.
func (i *Interpreter) lookUpVariable(name token.Token, e ast.Expr) any {
	if at, ok := i.locals[e]; ok {
		return i.environment.GetAt(at.distance, at.index)
	}
	value, err := i.globals.Get(name)
	if err != nil {
		i.failWith(err)
	}
	return value
}

func (i *Interpreter) fail(t token.Token, message string) {
	panic(runtimeSignal{err: &errors.RuntimeError{Token: t, Message: message}})
}

func (i *Interpreter) failWith(err *errors.RuntimeError) {
	panic(runtimeSignal{err: err})
}

func (i *Interpreter) VisitAssignExpr(e *ast.Assign) any {
	value := i.evaluate(e.Value)
	if at, ok := i.locals[e]; ok {
		i.environment.AssignAt(at.distance, at.index, value)
		return value
	}
	if err := i.globals.Assign(e.Name, value); err != nil {
		i.failWith(err)
	}
	return value
}

func (i *Interpreter) VisitCallExpr(e *ast.Call) any {
	callee := i.evaluate(e.Callee)
	arguments := make([]any, 0, len(e.Arguments))
	for _, argument := range e.Arguments {
		arguments = append(arguments, i.evaluate(argument))
	}

	callable, ok := callee.(Callable)
	if !ok {
		i.fail(e.Paren, "Can only call functions and classes.")
	}
	if len(arguments) != callable.Arity() {
		i.fail(e.Paren, fmt.Sprintf("Expected %d arguments but got %d.", callable.Arity(), len(arguments)))
	}
	return callable.Call(i, arguments)
}

func (i *Interpreter) VisitGetExpr(e *ast.Get) any {
	object := i.evaluate(e.Object)
	// A propertyOwner is an instance or a class; the switch is on the interface
	// so class methods need no separate path. Numbers and strings own no
	// properties here, deliberately: giving them any would mean deciding where
	// the methods of a primitive live, which is a bigger design question than
	// this chapter needs.
	owner, ok := object.(propertyOwner)
	if !ok {
		i.fail(e.Name, "Only instances have properties.")
	}
	value, err := owner.get(i, e.Name)
	if err != nil {
		i.failWith(err)
	}
	return value
}

// VisitSetExpr evaluates the object before the value, which is the order the
// two subexpressions appear in the source. It matters when both have side
// effects and the object turns out not to be an instance: the error is reported
// before the value is ever evaluated.
func (i *Interpreter) VisitSetExpr(e *ast.Set) any {
	object := i.evaluate(e.Object)
	owner, ok := object.(propertyOwner)
	if !ok {
		i.fail(e.Name, "Only instances have fields.")
	}

	value := i.evaluate(e.Value)
	owner.set(e.Name, value)
	return value
}

// VisitSuperExpr looks the method up on the superclass and binds it to the
// current receiver — not to the superclass. That is what makes `super.method()`
// a call on this object that starts its search one class higher, rather than a
// call on some other object.
//
// Both operands are read by slot. `super` sits where the resolver put it, and
// the receiver one hop nearer: the `this` scope is nested directly inside the
// `super` scope and holds exactly one binding, so distance-1, index 0 is it
// from anywhere the two are visible at all.
//
// Fields are not consulted. `super.x` names a method by construction — an
// instance's fields belong to the instance, not to a class in its chain — which
// is why this does not go through lookUpProperty.
func (i *Interpreter) VisitSuperExpr(e *ast.Super) any {
	at, resolved := i.locals[e]
	if !resolved {
		// Unreachable from a script: the resolver refuses `super` outside a
		// subclass before anything runs. A bare REPL line skips resolution
		// altogether, though, so this is the one way an unresolved `super`
		// reaches the interpreter — and reading slot 0 of the globals, which
		// have no slots, would take the process down. Report it the way an
		// unresolved `this` already reports itself.
		i.failWith(undefinedVariable(e.Keyword))
	}
	superclass := i.environment.GetAt(at.distance, at.index).(*LoxClass)
	receiver := i.environment.GetAt(at.distance-1, 0)

	// Which half of the class the search uses follows the receiver, which is
	// what `this` already means here: an instance in a method, the class itself
	// in a class method. So `super` inside a class method reaches the
	// superclass's class methods, and nothing else has to know the difference.
	find := superclass.findMethod
	if _, onTheClass := receiver.(*LoxClass); onTheClass {
		find = superclass.findClassMethod
	}

	method := find(e.Method.Lexeme)
	if method == nil {
		i.fail(e.Method, "Undefined property '"+e.Method.Lexeme+"'.")
	}

	bound := method.bind(receiver)
	if method.declaration.IsGetter {
		// A getter reached through `super` still runs on access. Handing back
		// the function instead would make `super.area` and `this.area` two
		// different kinds of expression.
		return bound.Call(i, nil)
	}
	return bound
}

// VisitThisExpr reads `this` exactly like any other variable, because by the
// time execution starts it is one: LoxFunction.bind put it in a scope and the
// resolver recorded where.
func (i *Interpreter) VisitThisExpr(e *ast.This) any {
	return i.lookUpVariable(e.Keyword, e)
}

func (i *Interpreter) VisitLiteralExpr(e *ast.Literal) any {
	return e.Value
}

func (i *Interpreter) VisitVariableExpr(e *ast.Variable) any {
	return i.lookUpVariable(e.Name, e)
}

func (i *Interpreter) VisitGroupingExpr(e *ast.Grouping) any {
	return i.evaluate(e.Expression)
}

func (i *Interpreter) VisitLogicalExpr(e *ast.Logical) any {
	left := i.evaluate(e.Left)

	switch e.Operator.Type {
	case token.OR:
		if truthy(left) {
			return left
		}
	case token.AND:
		if !truthy(left) {
			return left
		}
	default:
		i.fail(e.Operator, "Unsupported logical operator.")
	}

	return i.evaluate(e.Right)
}

func (i *Interpreter) VisitUnaryExpr(e *ast.Unary) any {
	right := i.evaluate(e.Right)

	switch e.Operator.Type {
	case token.BANG:
		return !truthy(right)
	case token.MINUS:
		return -i.number(e.Operator, right)
	}

	i.fail(e.Operator, "Unsupported unary operator.")
	return nil
}

func (i *Interpreter) VisitBinaryExpr(e *ast.Binary) any {
	left := i.evaluate(e.Left)
	right := i.evaluate(e.Right)

	switch e.Operator.Type {
	case token.MINUS:
		l, r := i.numbers(e.Operator, left, right)
		return l - r
	case token.SLASH:
		l, r := i.numbers(e.Operator, left, right)
		return l / r
	case token.STAR:
		l, r := i.numbers(e.Operator, left, right)
		return l * r
	case token.PLUS:
		switch l := left.(type) {
		case float64:
			if r, ok := right.(float64); ok {
				return l + r
			}
		case string:
			if r, ok := right.(string); ok {
				return l + r
			}
		}
		i.fail(e.Operator, "Operands must be two numbers or two strings.")
	case token.GREATER:
		l, r := i.numbers(e.Operator, left, right)
		return l > r
	case token.GREATER_EQUAL:
		l, r := i.numbers(e.Operator, left, right)
		return l >= r
	case token.LESS:
		l, r := i.numbers(e.Operator, left, right)
		return l < r
	case token.LESS_EQUAL:
		l, r := i.numbers(e.Operator, left, right)
		return l <= r
	case token.BANG_EQUAL:
		return !equal(left, right)
	case token.EQUAL_EQUAL:
		return equal(left, right)
	}

	i.fail(e.Operator, "Unsupported binary operator.")
	return nil
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	default:
		return true
	}
}

func equal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

func (i *Interpreter) number(t token.Token, v any) float64 {
	n, ok := v.(float64)
	if !ok {
		i.fail(t, "Operand must be a number.")
	}
	return n
}

func (i *Interpreter) numbers(t token.Token, a, b any) (float64, float64) {
	l, lok := a.(float64)
	r, rok := b.(float64)
	if !lok || !rok {
		i.fail(t, "Operands must be numbers.")
	}
	return l, r
}

func (i *Interpreter) VisitBlockStmt(stmt *ast.Block) any {
	i.executeBlock(stmt.Statements, NewEnvironment(i.environment))
	return nil
}

// VisitClassStmt turns the declaration into one runtime object and binds it
// under the class's name. Every method closes over the environment the class was
// declared in, so a method body can name the class itself — the binding below
// has completed by the time any method can be called.
//
// The book defines the name as nil, builds the class, then assigns over it.
// That two-step does not survive slot-addressed locals: Environment.Assign
// matches by name and a local environment holds no names, so it would walk past
// this scope and fail in the globals. One Define is correct here and keeps the
// resolver's slot numbering, because a class declaration performs exactly one
// Define in its scope.
//
// A superclass adds one environment between that scope and the methods, holding
// `super` alone — the runtime half of the scope resolver.VisitClassStmt opens.
// It is built as a local variable rather than by swapping i.environment, so the
// single Define of the class name still lands in the enclosing scope, in the
// slot the resolver numbered for it.
func (i *Interpreter) VisitClassStmt(stmt *ast.Class) any {
	var superclass *LoxClass
	if stmt.Superclass != nil {
		// The check is here and not in the resolver because the superclass is
		// an expression: the text only says which variable to read, and what it
		// holds is not known until this line runs.
		value := i.evaluate(stmt.Superclass)
		found, ok := value.(*LoxClass)
		if !ok {
			i.fail(stmt.Superclass.Name, "Superclass must be a class.")
		}
		superclass = found
	}

	// Methods close over the `super` environment when there is one, so the
	// distance a method body counts to `super` is fixed at declaration time —
	// like every other captured variable, and unlike the receiver, which is not
	// known until the method is found.
	methodEnvironment := i.environment
	if superclass != nil {
		methodEnvironment = NewEnvironment(i.environment)
		methodEnvironment.Define("super", superclass)
	}

	methods := make(map[string]*LoxFunction, len(stmt.Methods))
	for _, method := range stmt.Methods {
		// "init" is a name, not a keyword. Nothing stops a program from
		// declaring it, and nothing should: that is how you get a constructor.
		isInitializer := method.Name.Lexeme == "init"
		methods[method.Name.Lexeme] = newLoxFunction(method, methodEnvironment, isInitializer)
	}

	// A class method named init is not a constructor — construction is a thing
	// that happens to instances, and a class method never receives one.
	classMethods := make(map[string]*LoxFunction, len(stmt.ClassMethods))
	for _, method := range stmt.ClassMethods {
		classMethods[method.Name.Lexeme] = newLoxFunction(method, methodEnvironment, false)
	}

	i.environment.Define(stmt.Name.Lexeme, newLoxClass(stmt.Name.Lexeme, superclass, methods, classMethods))
	return nil
}

func (i *Interpreter) VisitExpressionStmt(stmt *ast.Expression) any {
	i.evaluate(stmt.Expression)
	return nil
}

func (i *Interpreter) VisitFunctionStmt(stmt *ast.Function) any {
	function := newLoxFunction(stmt, i.environment, false)
	i.environment.Define(stmt.Name.Lexeme, function)
	return nil
}

func (i *Interpreter) VisitIfStmt(stmt *ast.If) any {
	if truthy(i.evaluate(stmt.Condition)) {
		i.execute(stmt.ThenBranch)
	} else if stmt.ElseBranch != nil {
		i.execute(stmt.ElseBranch)
	}
	return nil
}

func (i *Interpreter) VisitPrintStmt(stmt *ast.Print) any {
	value := i.evaluate(stmt.Expression)
	fmt.Fprintln(i.out, ast.Stringify(value))
	return nil
}

func (i *Interpreter) VisitReturnStmt(stmt *ast.Return) any {
	var value any
	if stmt.Value != nil {
		value = i.evaluate(stmt.Value)
	}
	panic(returnSignal{value: value})
}

func (i *Interpreter) VisitVarStmt(stmt *ast.Var) any {
	var value any
	if stmt.Initializer != nil {
		value = i.evaluate(stmt.Initializer)
	}
	i.environment.Define(stmt.Name.Lexeme, value)
	return nil
}

func (i *Interpreter) VisitWhileStmt(stmt *ast.While) any {
	for truthy(i.evaluate(stmt.Condition)) {
		i.execute(stmt.Body)
	}
	return nil
}

func (i *Interpreter) executeBlock(statements []ast.Stmt, environment *Environment) {
	previous := i.environment
	i.environment = environment
	defer func() { i.environment = previous }()

	for _, statement := range statements {
		i.execute(statement)
	}
}
