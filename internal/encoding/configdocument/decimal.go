package configdocument

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func newJSONNumber(raw string) (Value, error) {
	if len(raw) == 0 || strings.TrimSpace(raw) != raw || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') || !json.Valid([]byte(raw)) {
		return Value{}, fmt.Errorf("%w: malformed JSON number", ErrInvalidValue)
	}
	negative := raw[0] == '-'
	unsigned := strings.TrimPrefix(raw, "-")
	mantissa, exponent, found := strings.Cut(strings.ToLower(unsigned), "e")
	if !found {
		exponent = "0"
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		if negative {
			return Value{data: jsonNumberNode("-0")}, nil
		}
		return Value{data: jsonNumberNode("0")}, nil
	}
	significant := strings.TrimRight(digits, "0")
	shift := len(digits) - len(significant) - len(fraction)
	exponent = addDecimalExponent(exponent, shift)
	normalized := significant
	if negative {
		normalized = "-" + normalized
	}
	if exponent != "0" {
		normalized += "e" + exponent
	}
	return Value{data: jsonNumberNode(normalized)}, nil
}

// Exponents stay decimal strings: a short input such as 1e999999999999 must
// never cause allocation of that magnitude or conversion through float64.
func addDecimalExponent(exponent string, delta int) string {
	negative := strings.HasPrefix(exponent, "-")
	magnitude := strings.TrimLeft(strings.TrimLeft(exponent, "+-"), "0")
	if magnitude == "" {
		magnitude = "0"
		negative = false
	}
	if delta == 0 {
		if negative {
			return "-" + magnitude
		}
		return magnitude
	}
	deltaNegative := delta < 0
	if deltaNegative {
		delta = -delta
	}
	other := strconv.Itoa(delta)
	var result string
	if negative == deltaNegative {
		result = addMagnitude(magnitude, other)
	} else {
		comparison := len(magnitude) - len(other)
		if comparison == 0 {
			comparison = strings.Compare(magnitude, other)
		}
		if comparison == 0 {
			return "0"
		}
		if comparison > 0 {
			result = subtractMagnitude(magnitude, other)
		} else {
			result = subtractMagnitude(other, magnitude)
			negative = deltaNegative
		}
	}
	if negative && result != "0" {
		return "-" + result
	}
	return result
}

func addMagnitude(left, right string) string {
	result := make([]byte, max(len(left), len(right))+1)
	carry := 0
	for leftIndex, rightIndex, out := len(left)-1, len(right)-1, len(result)-1; out >= 0; out-- {
		digit := carry
		if leftIndex >= 0 {
			digit += int(left[leftIndex] - '0')
			leftIndex--
		}
		if rightIndex >= 0 {
			digit += int(right[rightIndex] - '0')
			rightIndex--
		}
		result[out] = byte(digit%10) + '0'
		carry = digit / 10
	}
	return trimMagnitude(result)
}

func subtractMagnitude(left, right string) string {
	result := make([]byte, len(left))
	borrow := 0
	for leftIndex, rightIndex := len(left)-1, len(right)-1; leftIndex >= 0; leftIndex-- {
		digit := int(left[leftIndex]-'0') - borrow
		if rightIndex >= 0 {
			digit -= int(right[rightIndex] - '0')
			rightIndex--
		}
		borrow = 0
		if digit < 0 {
			digit += 10
			borrow = 1
		}
		result[leftIndex] = byte(digit) + '0'
	}
	return trimMagnitude(result)
}

func trimMagnitude(value []byte) string {
	result := strings.TrimLeft(string(value), "0")
	if result == "" {
		return "0"
	}
	return result
}
