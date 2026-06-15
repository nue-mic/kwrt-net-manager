package backup

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns a collision-resistant id with the given semantic prefix, e.g.
// NewID("ch") → "ch_9f3a1c0b7e21". 12 hex chars = 48 bits of randomness, plenty
// for a handful of channels/schedules/runs on a single host.
func NewID(prefix string) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is catastrophic; fall back to a fixed marker so the
		// caller still gets a usable (if non-unique) id rather than panicking.
		return prefix + "_000000000000"
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
