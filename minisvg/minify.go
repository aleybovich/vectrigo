package minisvg

import "regexp"

// decimalPattern matches a signed decimal number with a fractional part,
// e.g. "12.345" or "-0.5". Integers (no decimal point) never need rounding,
// so they are intentionally not matched.
var decimalPattern = regexp.MustCompile(`-?[0-9]+\.[0-9]+`)

// roundNumbers scans s for decimal numbers and rounds each one to precision
// fractional digits, leaving everything else untouched. precision must be
// >= 0.
func roundNumbers(s string, precision int) string {
	return decimalPattern.ReplaceAllStringFunc(s, func(m string) string {
		return roundDecimalString(m, precision)
	})
}

// roundDecimalString rounds a single decimal number, given as exact text
// (sign + integer part + '.' + fractional part), to precision fractional
// digits using round-half-away-from-zero semantics.
//
// The rounding is performed entirely on the decimal digit string rather than
// via float64 arithmetic, so it is immune to binary floating-point
// representation error (e.g. 1234.5/100 not being exactly representable):
// what you see in the input text is exactly what gets rounded.
//
// Trailing fractional zeros produced by rounding are trimmed (12.996 at
// precision 2 becomes "13", not "13.00"), and a spurious "-0" result is
// normalized to "0".
func roundDecimalString(numStr string, precision int) string {
	sign := ""
	if numStr[0] == '-' {
		sign = "-"
		numStr = numStr[1:]
	}

	dot := indexByte(numStr, '.')
	intPart := numStr[:dot]
	fracPart := numStr[dot+1:]

	var newInt, newFrac string
	if precision >= len(fracPart) {
		// Nothing to round off; keep as-is.
		newInt, newFrac = intPart, fracPart
	} else {
		keep := fracPart[:precision]
		roundUp := fracPart[precision] >= '5'

		digits := []byte(intPart + keep)
		if roundUp {
			digits = incrementDecimalDigits(digits)
		}
		newIntLen := len(digits) - len(keep)
		newInt = string(digits[:newIntLen])
		newFrac = string(digits[newIntLen:])
	}

	newInt = trimLeadingZeros(newInt)
	newFrac = trimTrailingZeros(newFrac)

	result := newInt
	if newFrac != "" {
		result += "." + newFrac
	}

	if isAllZeros(result) {
		return "0"
	}
	return sign + result
}

// incrementDecimalDigits adds 1 to the decimal number represented by digits
// (most-significant digit first), returning a new slice that may be one
// digit longer (e.g. "99" -> "100").
func incrementDecimalDigits(digits []byte) []byte {
	out := make([]byte, len(digits))
	copy(out, digits)

	carry := byte(1)
	for i := len(out) - 1; i >= 0 && carry > 0; i-- {
		d := out[i] - '0' + carry
		out[i] = d%10 + '0'
		carry = d / 10
	}
	if carry > 0 {
		out = append([]byte{'1'}, out...)
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimLeadingZeros(s string) string {
	i := 0
	for i < len(s)-1 && s[i] == '0' {
		i++
	}
	return s[i:]
}

func trimTrailingZeros(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == '0' {
		i--
	}
	return s[:i]
}

func isAllZeros(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' && s[i] != '.' {
			return false
		}
	}
	return true
}
