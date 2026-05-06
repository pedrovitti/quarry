package parse

import "github.com/pedrovitti/quarry/internal/lex"

// precedence orders infix operators. Higher binds tighter.
type precedence int

const (
	lowest precedence = iota
	orPrec
	andPrec
	notPrec
	comparisonPrec // = <> < <= > >= and the postfix IS [NOT] NULL
	concatPrec     // ||
	additivePrec   // + -
	productPrec    // * / %
	unaryPrec      // -x +x
)

// precedenceOf returns the binding power of kind as an infix operator, or
// lowest for tokens that do not continue an expression.
//
// NOT sits between AND and comparison, which is what makes "NOT a = 1" mean
// "NOT (a = 1)" and "NOT a AND b" mean "(NOT a) AND b".
func precedenceOf(kind lex.Kind) precedence {
	switch kind {
	case lex.Or:
		return orPrec
	case lex.And:
		return andPrec
	case lex.Eq, lex.NotEq, lex.Less, lex.LessEq, lex.Greater, lex.GreaterEq, lex.Is:
		return comparisonPrec
	case lex.Concat:
		return concatPrec
	case lex.Plus, lex.Minus:
		return additivePrec
	case lex.Star, lex.Slash, lex.Percent:
		return productPrec
	}
	return lowest
}
