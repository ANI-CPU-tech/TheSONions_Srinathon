// Unit tests for the SPEC section 10 token codec (PR K1).
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecodeTokenRaw(t *testing.T) {
	for _, tok := range []string{"a", "user:1", "hello-world", "123", "x"} {
		got, err := decodeToken(tok)
		if err != nil {
			t.Errorf("decodeToken(%q) error: %v", tok, err)
			continue
		}
		if string(got) != tok {
			t.Errorf("decodeToken(%q) = %q", tok, got)
		}
	}
}

func TestDecodeTokenHex(t *testing.T) {
	cases := map[string][]byte{
		"x:":               {},
		"x:00":             {0x00},
		"x:61":             {'a'},
		"x:00ff7f":         {0x00, 0xff, 0x7f},
		"x:783a68656c6c6f": []byte("x:hello"),
	}
	for tok, want := range cases {
		got, err := decodeToken(tok)
		if err != nil {
			t.Errorf("decodeToken(%q) error: %v", tok, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("decodeToken(%q) = %v, want %v", tok, got, want)
		}
	}
}

func TestDecodeTokenRejects(t *testing.T) {
	bad := []string{
		"",      // empty token: the empty value is spelled x:
		"x:zz",  // not hex
		"x:AB",  // uppercase hex is not the SPEC spelling
		"x:abc", // odd length
		"a b",   // whitespace
		"a\tb",  // control character
	}
	for _, tok := range bad {
		if got, err := decodeToken(tok); err == nil {
			t.Errorf("decodeToken(%q) = %q, want usage error", tok, got)
		}
	}
}

func TestEncodeToken(t *testing.T) {
	cases := map[string]string{
		"":         "x:",                 // empty value spelling
		"a":        "a",                  // raw
		"user:1":   "user:1",             // raw
		"hi there": "x:6869207468657265", // space forces hex
		"a\x00b":   "x:610062",           // control byte forces hex
		"\x80":     "x:80",               // non-ASCII forces hex
		"x:hello":  "x:783a68656c6c6f",   // raw starting with x: would be ambiguous
	}
	for in, want := range cases {
		if got := encodeToken([]byte(in)); got != want {
			t.Errorf("encodeToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenRoundTrip(t *testing.T) {
	values := []string{"", "a", "hello world", "x:foo", "\x00\x01\xff", strings.Repeat("v", 4096)}
	for _, v := range values {
		tok := encodeToken([]byte(v))
		got, err := decodeToken(tok)
		if err != nil {
			t.Fatalf("decodeToken(encodeToken(%q)=%q): %v", v, tok, err)
		}
		if string(got) != v {
			t.Errorf("round trip of %q via %q = %q", v, tok, got)
		}
	}
}
