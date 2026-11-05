package rank

import (
	"errors"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestBetween(t *testing.T) {
	for _, p := range [][2]string{{"", ""}, {"", "a"}, {"a", "b"}, {"a", "a1"}, {"a0", "a1"}, {"z", ""}, {"100", "150"}, {"a", "a0v"}} {
		r, e := Between(p[0], p[1])
		if e != nil || r <= p[0] || (p[1] != "" && r >= p[1]) {
			t.Fatal(p, r, e)
		}
	}
	_, e := Between("a", "a0")
	var se *SpaceError
	if !errors.As(e, &se) {
		t.Fatal(e)
	}
}

func TestLowercaseWithHumanBoundaries(t *testing.T) {
	for _, bounds := range [][2]string{{"", "A"}, {"A", "z"}, {"9Z", "a"}, {"0Z", "0a0"}, {"!", "?"}, {"A", "A0"}} {
		key, err := Between(bounds[0], bounds[1])
		if err != nil {
			var space *SpaceError
			if !errors.As(err, &space) {
				t.Fatal(err)
			}
			continue
		}
		if key <= bounds[0] || bounds[1] != "" && key >= bounds[1] || strings.Trim(key, Alphabet) != "" {
			t.Fatalf("invalid generated rank %q between %q and %q", key, bounds[0], bounds[1])
		}
	}
	keys := Rebalance(10000)
	for n, key := range keys {
		if strings.Trim(key, Alphabet) != "" || n > 0 && keys[n-1] >= key {
			t.Fatal("rebalance must be lowercase and strictly increasing", n, key)
		}
	}
}
func TestRandomInsertions(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	var keys []string
	for n := 0; n < 10000; n++ {
		at := r.Intn(len(keys) + 1)
		a, b := "", ""
		if at > 0 {
			a = keys[at-1]
		}
		if at < len(keys) {
			b = keys[at]
		}
		k, e := Between(a, b)
		if e != nil {
			t.Fatal(n, a, b, e)
		}
		if NeedsRebalance(k) {
			keys = Rebalance(len(keys))
			n--
			continue
		}
		keys = append(keys, "")
		copy(keys[at+1:], keys[at:])
		keys[at] = k
		if !sort.StringsAreSorted(keys) {
			t.Fatal(n)
		}
	}
}
