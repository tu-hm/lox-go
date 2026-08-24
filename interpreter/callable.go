package interpreter

import "time"

// Callable is the runtime protocol shared by native functions, user-defined
// functions, and later chapters' classes.
type Callable interface {
	Arity() int
	Call(interpreter *Interpreter, arguments []any) any
}

type nativeClock struct{}

var _ Callable = nativeClock{}

func (nativeClock) Arity() int { return 0 }

func (nativeClock) Call(_ *Interpreter, _ []any) any {
	return float64(time.Now().UnixNano()) / float64(time.Second)
}

func (nativeClock) String() string { return "<native fn>" }
