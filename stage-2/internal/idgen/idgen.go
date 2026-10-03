// Package idgen creates opaque identifiers and booking references.
package idgen

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"sync/atomic"
)

var counter uint64

const refAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// New returns an opaque, process-unique ID with the given prefix, at most 64 chars.
func New(prefix string) string {
	n := atomic.AddUint64(&counter, 1)
	var buf [10]byte
	_, _ = rand.Read(buf[:])
	rnd := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf[:])
	rnd = strings.ToLower(rnd)
	return fmt.Sprintf("%s_%d%s", prefix, n, rnd)
}

// Reference returns a random 8-character A-Z0-9 booking reference candidate.
// Callers must check uniqueness against existing reservations and retry on collision.
func Reference() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	out := make([]byte, 8)
	for i, b := range buf {
		out[i] = refAlphabet[int(b)%len(refAlphabet)]
	}
	return string(out)
}

// Token returns a random bearer token.
func Token() string {
	var buf [32]byte
	_, _ = rand.Read(buf[:])
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf[:])
}
