package ids

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func New(prefix string) (string, error) {
	return NewAt(prefix, time.Now().UTC())
}

func NewAt(prefix string, t time.Time) (string, error) {
	if err := validatePrefix(prefix); err != nil {
		return "", err
	}
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("read random entropy: %w", err)
	}
	return prefix + "_" + encodeULIDTimeEntropy(t, entropy), nil
}

func validatePrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("id prefix is required")
	}
	for i, r := range prefix {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return fmt.Errorf("invalid id prefix %q", prefix)
	}
	return nil
}

func encodeULIDTimeEntropy(t time.Time, entropy [10]byte) string {
	var data [16]byte
	ms := uint64(t.UnixMilli())
	data[0] = byte(ms >> 40)
	data[1] = byte(ms >> 32)
	data[2] = byte(ms >> 24)
	data[3] = byte(ms >> 16)
	data[4] = byte(ms >> 8)
	data[5] = byte(ms)
	copy(data[6:], entropy[:])
	return encodeCrockford128(data)
}

func encodeCrockford128(data [16]byte) string {
	hi := binary.BigEndian.Uint64(data[0:8])
	lo := binary.BigEndian.Uint64(data[8:16])
	var out [26]byte
	for i := 25; i >= 0; i-- {
		out[i] = alphabet[lo&31]
		lo = (lo >> 5) | ((hi & 31) << 59)
		hi >>= 5
	}
	return string(out[:])
}

func HasPrefix(id, prefix string) bool {
	return strings.HasPrefix(id, prefix+"_")
}
