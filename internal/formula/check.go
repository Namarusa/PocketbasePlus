package formula

import (
	"strings"
)

// The operators of the conditions built without formulas (the page
// conditions): "if image is empty", "if hp greater than 100"...
const (
	OpEmpty       = "empty"
	OpNotEmpty    = "notEmpty"
	OpEquals      = "eq"
	OpNotEquals   = "ne"
	OpGreater     = "gt"
	OpGreaterEq   = "gte"
	OpLess        = "lt"
	OpLessEq      = "lte"
	OpContains    = "contains"
	OpNotContains = "notContains"
	OpStartsWith  = "startsWith"
	OpEndsWith    = "endsWith"
	OpTrue        = "isTrue"
	OpFalse       = "isFalse"
)

// IsOp reports whether op is an operator of Check.
func IsOp(op string) bool {
	switch op {
	case OpEmpty, OpNotEmpty, OpEquals, OpNotEquals, OpGreater, OpGreaterEq, OpLess, OpLessEq,
		OpContains, OpNotContains, OpStartsWith, OpEndsWith, OpTrue, OpFalse:
		return true
	}
	return false
}

// Check reports whether a value passes a condition: value op right (the
// right side is a text, compared as a number or a date when the value is
// one). Texts are compared ignoring case and surrounding spaces; a list
// contains an item, equals none.
func Check(op string, value any, right string) bool {
	switch op {
	case OpEmpty:
		return isEmpty(value)
	case OpNotEmpty:
		return !isEmpty(value)
	case OpEquals:
		return sameValue(value, right)
	case OpNotEquals:
		return !sameValue(value, right)
	case OpGreater, OpGreaterEq, OpLess, OpLessEq:
		if isEmpty(value) || strings.TrimSpace(right) == "" {
			return false // nothing to compare: no order
		}
		c := compare(value, strings.TrimSpace(right))
		switch op {
		case OpGreater:
			return c > 0
		case OpGreaterEq:
			return c >= 0
		case OpLess:
			return c < 0
		}
		return c <= 0
	case OpContains:
		return containsFold(value, right)
	case OpNotContains:
		return !containsFold(value, right)
	case OpStartsWith:
		return strings.HasPrefix(strings.ToLower(toStr(value)), strings.ToLower(right))
	case OpEndsWith:
		return strings.HasSuffix(strings.ToLower(toStr(value)), strings.ToLower(right))
	case OpTrue:
		return truthy(value) && !falseText(value)
	case OpFalse:
		return !truthy(value) || falseText(value)
	}
	return false
}

// a text that says false ("false", "no", "0")
func falseText(v any) bool {
	s, ok := plain(v).(string)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "false", "no", "0", "off":
		return true
	}
	return false
}

func sameValue(v any, right string) bool {
	right = strings.TrimSpace(right)
	if isList(v) {
		return false
	}
	_, num := toFloat(v)
	_, rnum := toFloat(right)
	if (num && rnum) || (isDate(v) && right != "") {
		return compare(v, right) == 0
	}
	if b, ok := plain(v).(bool); ok {
		return b == (truthy(right) && !falseText(right))
	}
	return strings.EqualFold(strings.TrimSpace(toStr(v)), right)
}

func containsFold(v any, part string) bool {
	if isList(v) {
		for _, item := range toList(v) {
			if sameValue(item, part) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(toStr(v)), strings.ToLower(strings.TrimSpace(part)))
}
