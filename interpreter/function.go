package interpreter

import "compiler101/ast"

// LoxFunction is the runtime form of a function declaration. Closure is the
// environment active at declaration time, not at the later call site.
type LoxFunction struct {
	declaration *ast.Function
	closure     *Environment
}

var _ Callable = (*LoxFunction)(nil)

func newLoxFunction(declaration *ast.Function, closure *Environment) *LoxFunction {
	return &LoxFunction{declaration: declaration, closure: closure}
}

func (f *LoxFunction) Arity() int { return len(f.declaration.Params) }

func (f *LoxFunction) Call(interpreter *Interpreter, arguments []any) (result any) {
	environment := NewEnvironment(f.closure)
	for index, parameter := range f.declaration.Params {
		environment.Define(parameter.Lexeme, arguments[index])
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			if returned, ok := recovered.(returnSignal); ok {
				result = returned.value
				return
			}
			panic(recovered)
		}
	}()
	interpreter.executeBlock(f.declaration.Body, environment)
	return nil
}

func (f *LoxFunction) String() string { return "<fn " + f.declaration.Name.Lexeme + ">" }

type returnSignal struct{ value any }
