// Token encoding (docs/SPEC.md section 10). The torture tool always sends
// x:<hex>; dump may print raw text, so decoding accepts both forms.
package main

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// encodeToken returns the x:<hex> form of b. The empty value is "x:".
func encodeToken(b []byte) string { return "x:" + hex.EncodeToString(b) }

// decodeToken turns a dump token back into bytes: x:<hex>, or raw text.
func decodeToken(tok string) ([]byte, error) {
	if h, ok := strings.CutPrefix(tok, "x:"); ok {
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, fmt.Errorf("bad hex token %q: %v", tok, err)
		}
		return b, nil
	}
	if tok == "" {
		return nil, fmt.Errorf("empty token")
	}
	return []byte(tok), nil
}
