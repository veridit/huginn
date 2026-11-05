package rank

import (
	"errors"
	"math/rand"
	"sort"
	"testing"
)

func TestBetween(t *testing.T) {
	for _, p := range [][2]string{{"", ""}, {"", "A"}, {"A", "B"}, {"A", "A1"}, {"A0", "A1"}, {"z", ""}, {"100", "150"}, {"A", "A0V"}} {
		r, e := Between(p[0], p[1])
		if e != nil || r <= p[0] || (p[1] != "" && r >= p[1]) {
			t.Fatal(p, r, e)
		}
	}
	_, e := Between("A", "A0")
	var se *SpaceError
	if !errors.As(e, &se) {
		t.Fatal(e)
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
