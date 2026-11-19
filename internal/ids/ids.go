// Package ids generates identifiers for messages, sessions and turn tokens.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

var (
	mu     sync.Mutex
	lastMS int64
	seq    uint32
)

// New returns a unique id that sorts in creation order within a process:
// 12 hex digits of unix milliseconds, a 4-hex-digit sequence for ids made in
// the same millisecond, and 4 random hex digits.
func New() string {
	mu.Lock()
	ms := time.Now().UnixMilli()
	if ms <= lastMS {
		ms = lastMS
		seq++
		if seq > 0xffff {
			ms++
			seq = 0
		}
	} else {
		seq = 0
	}
	lastMS = ms
	id := fmt.Sprintf("%012x%04x%s", ms, seq, randomHex(2))
	mu.Unlock()
	return id
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
