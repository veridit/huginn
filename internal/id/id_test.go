package id

import (
	"bytes"
	"testing"
)

func TestGenerate(t *testing.T) {
	r := bytes.NewReader(append(make([]byte, 6), []byte{1, 2, 3, 4, 5, 6}...))
	s, e := Generate(r, map[string]bool{"000000": true})
	if e != nil || s != "123456" || !Valid(s) {
		t.Fatal(s, e)
	}
	if _, e = Generate(bytes.NewReader(nil), nil); e == nil {
		t.Fatal("missing entropy")
	}
	for n := 0; n < 100; n++ {
		s, e = New(nil)
		if e != nil || !Valid(s) {
			t.Fatal(s, e)
		}
	}
}
