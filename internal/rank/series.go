package rank

// Series generates n balanced keys inside an interval. The caller widens its
// repair window when this interval cannot hold keys within maxLength.
func Series(a, b string, n, maxLength int) ([]string, error) {
	if n == 0 {
		return []string{}, nil
	}
	middle, e := Between(a, b)
	if e != nil {
		return nil, e
	}
	if len(middle) > maxLength {
		return nil, &SpaceError{a, b}
	}
	leftCount := n / 2
	left, e := Series(a, middle, leftCount, maxLength)
	if e != nil {
		return nil, e
	}
	right, e := Series(middle, b, n-leftCount-1, maxLength)
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, n)
	out = append(out, left...)
	out = append(out, middle)
	out = append(out, right...)
	return out, nil
}
