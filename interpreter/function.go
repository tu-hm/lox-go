package interpreter

import "compiler101/ast"

// LoxFunction is the runtime form of a function declaration. Closure is the
// environment active at declaration time, not at the later call site.
type LoxFunction struct {
	declaration *ast.Function
	closure     *Environment
	// isInitializer makes a call return the receiver instead of the body's
	// value. It is a property of how the function was declared, not of the
	// call, which is why it lives here and not in Call's arguments.
	isInitializer bool
}

var _ Callable = (*LoxFunction)(nil)

func newLoxFunction(declaration *ast.Function, closure *Environment, isInitializer bool) *LoxFunction {
	return &LoxFunction{declaration: declaration, closure: closure, isInitializer: isInitializer}
}

func (f *LoxFunction) Arity() int { return len(f.declaration.Params) }

// bind returns the same function closed over one more scope, holding the
// receiver. That is the whole implementation of `this`: it is an ordinary
// captured variable in an environment the method did not write.
//
// The single Define lands in slot 0 because a local environment appends, and
// slot 0 is exactly where the resolver looks — it opens one scope per class and
// declares `this` first in it. The two have to agree; see resolver.VisitClassStmt.
func (f *LoxFunction) bind(instance *LoxInstance) *LoxFunction {
	environment := NewEnvironment(f.closure)
	environment.Define("this", instance)
	return newLoxFunction(f.declaration, environment, f.isInitializer)
}

func (f *LoxFunction) Call(interpreter *Interpreter, arguments []any) (result any) {
	environment := NewEnvironment(f.closure)
	for index, parameter := range f.declaration.Params {
		environment.Define(parameter.Lexeme, arguments[index])
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			if returned, ok := recovered.(returnSignal); ok {
				result = returned.value
				if f.isInitializer {
					result = f.receiver()
				}
				return
			}
			panic(recovered)
		}
	}()
	interpreter.executeBlock(f.declaration.Body, environment)
	if f.isInitializer {
		return f.receiver()
	}
	return nil
}

// receiver reads back the `this` that bind captured. An initializer is only
// ever called through bind, so its closure *is* the environment bind made, and
// that environment holds exactly one slot.
func (f *LoxFunction) receiver() any { return f.closure.GetAt(0, 0) }

func (f *LoxFunction) String() string { return "<fn " + f.declaration.Name.Lexeme + ">" }

type returnSignal struct{ value any }
