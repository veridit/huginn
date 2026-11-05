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
	if b != "" && a >= b {
		return "", &SpaceError{a, b}
	}
	// Extending a is always greater than a. Seek the first digit whose
	// appended key lies below the upper boundary, extending common prefixes.
	p := ""
	for i := 0; i < 1024; i++ {
		lo := -1
		hi := 62
		if i < len(a) {
			lo = strings.IndexByte(Alphabet, a[i])
			if lo < 0 {
				return "", &SpaceError{a, b}
			}
		}
		if b != "" && i < len(b) {
			hi = strings.IndexByte(Alphabet, b[i])
			if hi < 0 {
				return "", &SpaceError{a, b}
			}
		} else if b != "" && strings.HasPrefix(p, b) {
			return "", &SpaceError{a, b}
		}
		if hi-lo > 1 {
			v := p + string(Alphabet[(lo+hi)/2])
			if v > a && (b == "" || v < b) {
				return v, nil
			}
		}
		if i < len(a) {
			p += string(a[i])
		} else if hi > 0 {
			p += "0"
		} else {
			return "", &SpaceError{a, b}
		}
		if b != "" && p < b && !strings.HasPrefix(b, p) {
			b = ""
		}
	}
	return "", &SpaceError{a, b}
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

// Rebalance returns evenly spaced fixed-width keys in the supplied order.
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
		out[i] = string(b)
	}
	return out
}
