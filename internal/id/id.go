// Package id generates immutable task identifiers.
package id

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

const Alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

func Valid(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(Alphabet, c) {
			return false
		}
	}
	return true
}
func New(known map[string]bool) (string, error) { return Generate(rand.Reader, known) }
func Generate(r io.Reader, known map[string]bool) (string, error) {
	for attempt := 0; attempt < 1024; attempt++ {
		var b [6]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return "", err
		}
		for i := range b {
			b[i] = Alphabet[b[i]&31]
		}
		s := string(b[:])
		if !known[s] {
			return s, nil
		}
	}
	return "", fmt.Errorf("id collision retry limit reached")
}
