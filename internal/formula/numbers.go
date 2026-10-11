package formula

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// round rounds to a number of decimals: "round value" (whole number) or
// "round decimals value".
func round(args ...any) (Number, error) {
	switch len(args) {
	case 1:
		return Number(math.Round(num(args[0]))), nil
	case 2:
		p := math.Pow(10, float64(max(0, min(toInt(args[0]), 10))))
		return Number(math.Round(num(args[1])*p) / p), nil
	}
	return 0, errors.New("round: 1 or 2 values expected (decimals, value)")
}

func sum(values ...any) Number {
	total := 0.0
	for _, v := range flatten(values) {
		total += num(v)
	}
	return Number(total)
}

func avg(values ...any) any {
	var nums []float64
	for _, v := range flatten(values) {
		if f, ok := toFloat(v); ok {
			nums = append(nums, f)
		}
	}
	if len(nums) == 0 {
		return ""
	}
	total := 0.0
	for _, f := range nums {
		total += f
	}
	return Number(total / float64(len(nums)))
}

func extreme(values []any, less bool) any {
	var best any
	for _, v := range flatten(values) {
		if v == nil || v == "" {
			continue
		}
		if best == nil {
			best = v
			continue
		}
		c := compare(v, best)
		if (less && c < 0) || (!less && c > 0) {
			best = v
		}
	}
	if best == nil {
		return ""
	}
	if f, ok := toFloat(best); ok {
		return Number(f)
	}
	return best
}

// flatten returns the values, the items of the lists among them inlined.
func flatten(values []any) []any {
	var out []any
	for _, v := range values {
		if isList(v) {
			out = append(out, toList(v)...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

func subtract(a, b any) Number { return Number(num(a) - num(b)) }

func multiply(values ...any) Number {
	if len(values) == 0 {
		return 0
	}
	total := 1.0
	for _, v := range values {
		total *= num(v)
	}
	return Number(total)
}

func divide(a, b any) (Number, error) {
	d := num(b)
	if d == 0 {
		return 0, errors.New("division by zero")
	}
	return Number(num(a) / d), nil
}

func modulo(a, b any) (Number, error) {
	d := num(b)
	if d == 0 {
		return 0, errors.New("division by zero")
	}
	return Number(math.Mod(num(a), d)), nil
}

func percent(part, total any) any {
	t := num(total)
	if t == 0 {
		return ""
	}
	return Number(num(part) / t * 100)
}

// formatNumber writes a number with thousands separators: "formatNumber
// value" or "formatNumber decimals value".
func formatNumber(args ...any) (string, error) {
	switch len(args) {
	case 1:
		f, ok := toFloat(args[0])
		if !ok {
			return "", nil
		}
		decimals := 0
		if f != math.Trunc(f) {
			decimals = 2
		}
		return groupDigits(f, decimals), nil
	case 2:
		f, ok := toFloat(args[1])
		if !ok {
			return "", nil
		}
		return groupDigits(f, max(0, min(toInt(args[0]), 10))), nil
	}
	return "", errors.New("formatNumber: 1 or 2 values expected (decimals, value)")
}

func groupDigits(f float64, decimals int) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return ""
	}
	s := strconv.FormatFloat(math.Abs(f), 'f', decimals, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if f < 0 && strings.Trim(s, "0.") != "" {
		b.WriteByte('-')
	}
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

var currencySymbols = map[string]string{"USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥", "CNY": "¥", "INR": "₹", "KRW": "₩", "BRL": "R$", "CAD": "CA$", "AUD": "A$", "CHF": "CHF ", "MAD": "MAD "}

// currency writes an amount with the currency's symbol ("€1,234.50").
func currency(code, v any) string {
	f, ok := toFloat(v)
	if !ok {
		return ""
	}
	c := strings.ToUpper(strings.TrimSpace(toStr(code)))
	decimals := 2
	if c == "JPY" || c == "KRW" {
		decimals = 0
	}
	amount := groupDigits(math.Abs(f), decimals)
	sign := ""
	if f < 0 && amount != groupDigits(0, decimals) {
		sign = "-"
	}
	if sym, ok := currencySymbols[c]; ok {
		return sign + sym + amount
	}
	if c == "" {
		return sign + amount
	}
	return sign + c + " " + amount
}

// fileSize writes a number of bytes as "1.5 MB".
func fileSize(v any) string {
	f, ok := toFloat(v)
	if !ok {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for math.Abs(f) >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return formatFloat(f) + " B"
	}
	return formatFloat(math.Round(f*10)/10) + " " + units[i]
}

// toNumber returns the number of a value (0 when it isn't one).
func toNumber(v any) Number {
	return Number(num(v))
}
