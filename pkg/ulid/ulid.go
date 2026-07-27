// Package ulid provides a small, dependency-free ULID generator used as the
// primary key for persisted records. It mirrors the canonical ULID spec
// (48-bit millisecond timestamp + 80-bit randomness, Crockford base32, 26
// chars) so identifiers stay compatible with records created by agent-frame.
package ulid

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// crockford is the Crockford base32 alphabet (no I, L, O, U).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	mu       sync.Mutex
	lastMS   uint64
	lastRand [10]byte
)

// New returns a new 26-character ULID string. It is monotonic within the same
// millisecond (the random component is incremented) and safe for concurrent use.
func New() string {
	ms := uint64(time.Now().UnixMilli())

	mu.Lock()
	var entropy [10]byte
	if ms == lastMS {
		// Same millisecond: increment the previous randomness to preserve order.
		lastRand = increment(lastRand)
		entropy = lastRand
	} else {
		lastMS = ms
		_, _ = rand.Read(entropy[:])
		lastRand = entropy
	}
	mu.Unlock()

	return encode(ms, entropy)
}

// increment adds 1 to an 80-bit big-endian value, wrapping on overflow.
func increment(b [10]byte) [10]byte {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			break
		}
	}
	return b
}

// encode renders the 48-bit timestamp and 80-bit entropy into 26 base32 chars.
func encode(ms uint64, entropy [10]byte) string {
	var raw [16]byte
	// 48-bit timestamp in the high 6 bytes.
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], ms)
	copy(raw[0:6], ts[2:8])
	copy(raw[6:16], entropy[:])

	// 128 bits -> 26 base32 symbols (5 bits each, top symbol uses 2 bits).
	out := make([]byte, 26)
	out[0] = crockford[(raw[0]&0xE0)>>5]
	out[1] = crockford[raw[0]&0x1F]
	out[2] = crockford[(raw[1]&0xF8)>>3]
	out[3] = crockford[((raw[1]&0x07)<<2)|((raw[2]&0xC0)>>6)]
	out[4] = crockford[(raw[2]&0x3E)>>1]
	out[5] = crockford[((raw[2]&0x01)<<4)|((raw[3]&0xF0)>>4)]
	out[6] = crockford[((raw[3]&0x0F)<<1)|((raw[4]&0x80)>>7)]
	out[7] = crockford[(raw[4]&0x7C)>>2]
	out[8] = crockford[((raw[4]&0x03)<<3)|((raw[5]&0xE0)>>5)]
	out[9] = crockford[raw[5]&0x1F]
	out[10] = crockford[(raw[6]&0xF8)>>3]
	out[11] = crockford[((raw[6]&0x07)<<2)|((raw[7]&0xC0)>>6)]
	out[12] = crockford[(raw[7]&0x3E)>>1]
	out[13] = crockford[((raw[7]&0x01)<<4)|((raw[8]&0xF0)>>4)]
	out[14] = crockford[((raw[8]&0x0F)<<1)|((raw[9]&0x80)>>7)]
	out[15] = crockford[(raw[9]&0x7C)>>2]
	out[16] = crockford[((raw[9]&0x03)<<3)|((raw[10]&0xE0)>>5)]
	out[17] = crockford[raw[10]&0x1F]
	out[18] = crockford[(raw[11]&0xF8)>>3]
	out[19] = crockford[((raw[11]&0x07)<<2)|((raw[12]&0xC0)>>6)]
	out[20] = crockford[(raw[12]&0x3E)>>1]
	out[21] = crockford[((raw[12]&0x01)<<4)|((raw[13]&0xF0)>>4)]
	out[22] = crockford[((raw[13]&0x0F)<<1)|((raw[14]&0x80)>>7)]
	out[23] = crockford[(raw[14]&0x7C)>>2]
	out[24] = crockford[((raw[14]&0x03)<<3)|((raw[15]&0xE0)>>5)]
	out[25] = crockford[raw[15]&0x1F]
	return string(out)
}
