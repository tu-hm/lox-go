package ast

import (
	"fmt"
	"strconv"
)

// Stringify renders a Lox value as source-like text.
//
// Chapter 5 uses it for Literal nodes; chapter 7's interpreter needs the exact
// same rules for runtime values, so it lives here rather than inside a single
// visitor. Numbers print without a trailing ".0" — Java's Double.toString gives
// the book "123.0", Go's FormatFloat with precision -1 gives "123". Either is
// fine as long as every consumer and test agrees, so there is one copy.
func Stringify(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
