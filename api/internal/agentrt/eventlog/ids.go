package eventlog

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// crockford is the ULID alphabet: no I, L, O or U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a 26-character ULID: 48 bits of milliseconds, 80 random.
func NewULID(now time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(now.UnixMilli())<<16)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	// 128 bits as 26 base-32 digits, most significant first; the first digit
	// carries the top 3 bits.
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])
	var out [26]byte
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}
