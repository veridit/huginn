// Package rank implements bytewise fractional ordering.
package rank

import (
	"fmt"
	"strings"
)

const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

type SpaceError struct{ Before, After string }

func (e *SpaceError) Error() string {
	return fmt.Sprintf("no base62 rank between %q and %q; move beside another neighbour", e.Before, e.After)
}
func Between(a, b string) (string, error) {
	originalA, originalB := a, b
	if b != "" && a >= b {
		return "", &SpaceError{a, b}
	}
	prefix := ""
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		prefix += a[:1]
		a = a[1:]
		b = b[1:]
	}
	if a == "" && b != "" && b[0] == '0' {
		if len(b) == 1 {
			return "", &SpaceError{originalA, originalB}
		}
		suffix, e := Between("", b[1:])
		if e != nil {
			return "", e
		}
		return prefix + "0" + suffix, nil
	}
	lo, hi := -1, 62
	if a != "" {
		lo = strings.IndexByte(Alphabet, a[0])
		if lo < 0 {
			return "", &SpaceError{originalA, originalB}
		}
	}
	if b != "" {
		hi = strings.IndexByte(Alphabet, b[0])
		if hi < 0 {
			return "", &SpaceError{originalA, originalB}
		}
	}
	if hi-lo > 1 {
		digit := Alphabet[(lo+hi)/2]
		key := prefix + string(digit)
		if digit == '0' {
			key += "U"
		}
		return key, nil
	}
	if a != "" {
		tail, e := Between(a[1:], "")
		if e != nil {
			return "", e
		}
		return prefix + a[:1] + tail, nil
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
	const space = uint64(56800235584)
	step := space / uint64(n+1)
	for i := range out {
		v := uint64(i+1) * step
		b := make([]byte, width)
		for j := width - 1; j >= 0; j-- {
			b[j] = Alphabet[v%62]
			v /= 62
		}
		out[i] = string(b) + "U"
	}
	return out
}
