// Token codec (docs/SPEC.md section 10): raw text or x:<lowercase hex>,
// empty value is x:, dump picks raw only for printable non-space ASCII. PR K1.
package main

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// tokenPrefix introduces the hex form of a token.
const tokenPrefix = "x:"

// decodeToken parses one key or value token exactly as SPEC section 10
// defines it: raw text with no whitespace that does not start with "x:",
// or "x:" followed by lowercase hex. "x:" is the empty byte string.
// A malformed token is a usage error (exit code 2); nothing has run yet.
func decodeToken(tok string) ([]byte, error) {
	if strings.HasPrefix(tok, tokenPrefix) {
		h := tok[len(tokenPrefix):]
		if len(h)%2 != 0 {
			return nil, usagef("token %s has odd-length hex", snippet(tok))
		}
		for i := 0; i < len(h); i++ {
			c := h[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return nil, usagef("token %s is not lowercase hex (SPEC section 10)", snippet(tok))
			}
		}
		b, err := hex.DecodeString(h) // cannot fail: checked above
		if err != nil {
			return nil, usagef("token %s: %v", snippet(tok), err)
		}
		return b, nil
	}
	if tok == "" {
		return nil, usagef("empty token (use %s for an empty value)", tokenPrefix)
	}
	for i := 0; i < len(tok); i++ {
		if tok[i] <= 0x20 { // space and every ASCII control character
			return nil, usagef("raw token %s contains whitespace", snippet(tok))
		}
	}
	return []byte(tok), nil
}

// encodeToken renders b for output: raw text when every byte is printable
// ASCII other than space and the text does not start with "x:" (which would
// make it look like hex), otherwise x:<lowercase hex>. The empty byte string
// is "x:", the documented spelling of the empty value.
func encodeToken(b []byte) string {
	if len(b) == 0 {
		return tokenPrefix
	}
	if isRawText(b) && !strings.HasPrefix(string(b), tokenPrefix) {
		return string(b)
	}
	return tokenPrefix + hex.EncodeToString(b)
}

// isRawText reports whether every byte of b is printable ASCII (0x21-0x7e),
// i.e. anything except space, control characters and non-ASCII bytes.
func isRawText(b []byte) bool {
	for _, c := range b {
		if c <= 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// snippet shortens a token for error messages so a 1 MiB value cannot flood
// the terminal.
func snippet(s string) string {
	const max = 32
	if len(s) <= max {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%q...", s[:max])
}
