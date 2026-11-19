// Package ids generates identifiers for messages, sessions and turn tokens.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New returns a time-sortable unique id: 12 hex digits of unix milliseconds
// followed by 8 random hex digits.
func New() string {
	return fmt.Sprintf("%012x%s", time.Now().UnixMilli(), randomHex(4))
}

// UUID returns a random RFC 4122 version 4 UUID.
func UUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Token returns a random 32-byte hex secret.
func Token() string {
	return randomHex(32)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
