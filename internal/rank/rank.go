// Package rank implements bytewise fractional ordering.
package rank

import (
	"fmt"
	"strings"
)

const Alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

type SpaceError struct{ Before, After string }

func (e *SpaceError) Error() string {
	return fmt.Sprintf("no lowercase base36 rank between %q and %q; move beside another neighbour", e.Before, e.After)
}
func Between(a, b string) (string, error) {
	originalA, originalB := a, b
	if b != "" && a >= b {
		return "", &SpaceError{a, b}
	}
	prefix := ""
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		if strings.IndexByte(Alphabet, a[0]) < 0 {
			return "", &SpaceError{originalA, originalB}
		}
		prefix += a[:1]
		a = a[1:]
		b = b[1:]
	}
	// Boundaries may contain arbitrary human-typed bytes. Only generated
	// prefixes/digits must belong to the lowercase alphabet.
	lo, hi := -1, 256
	if a != "" {
		lo = int(a[0])
	}
	if b != "" {
		hi = int(b[0])
	}
	var digits []byte
	for i := range Alphabet {
		if int(Alphabet[i]) > lo && int(Alphabet[i]) < hi {
			digits = append(digits, Alphabet[i])
		}
	}
	if len(digits) > 0 {
		digit := digits[(len(digits)-1)/2]
		key := prefix + string(digit)
		if digit == '0' {
			key += "i"
		}
		return key, nil
	}
	if a != "" && strings.IndexByte(Alphabet, a[0]) >= 0 {
		tail, e := Between(a[1:], "")
		if e == nil {
			return prefix + a[:1] + tail, nil
		}
	}
	if b != "" && strings.IndexByte(Alphabet, b[0]) >= 0 {
		tail, e := Between("", b[1:])
		if e == nil && b[1:] != "" {
			return prefix + b[:1] + tail, nil
		}
		if len(b) > 1 && b[0] != '0' {
			return prefix + b[:1], nil
		}
	}
	return "", &SpaceError{originalA, originalB}
}
func NeedsRebalance(s string) bool { return len(s) > 12 }
func Less(a, aid, b, bid string) bool {
	if a == b {
		return aid < bid
	}
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return a < b
}
func Rebalance(n int) []string {
	out := make([]string, n)
	const width = 6
	const space = uint64(2176782336)
	step := space / uint64(n+1)
	for i := range out {
		v := uint64(i+1) * step
		b := make([]byte, width)
		for j := width - 1; j >= 0; j-- {
			b[j] = Alphabet[v%36]
			v /= 36
		}
		out[i] = string(b) + "i"
	}
	return out
}
