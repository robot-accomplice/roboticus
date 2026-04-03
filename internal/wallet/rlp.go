package wallet

import (
	"math/big"
)

// rlpEncodeList RLP-encodes a list of items and returns the complete encoding
// including the list prefix. Each item can be:
//   - []byte: encoded as an RLP string
//   - *big.Int: encoded as a big-endian byte string (zero -> empty string)
//   - uint64: encoded as a big-endian byte string (zero -> empty string)
//   - []any: encoded as a nested list
func rlpEncodeList(items []any) []byte {
	var payload []byte
	for _, item := range items {
		payload = append(payload, rlpEncodeItem(item)...)
	}
	return rlpPrependListHeader(payload)
}

// rlpEncodeItem encodes a single item according to RLP rules.
func rlpEncodeItem(item any) []byte {
	switch v := item.(type) {
	case []byte:
		return rlpEncodeBytes(v)
	case *big.Int:
		if v == nil || v.Sign() == 0 {
			return rlpEncodeBytes(nil)
		}
		return rlpEncodeBytes(v.Bytes())
	case uint64:
		if v == 0 {
			return rlpEncodeBytes(nil)
		}
		return rlpEncodeBytes(uint64ToMinBytes(v))
	case []any:
		return rlpEncodeList(v)
	default:
		// Treat unknown types as empty bytes.
		return rlpEncodeBytes(nil)
	}
}

// rlpEncodeBytes RLP-encodes a byte slice as a string.
func rlpEncodeBytes(b []byte) []byte {
	if len(b) == 0 {
		// Empty string: 0x80
		return []byte{0x80}
	}
	if len(b) == 1 && b[0] < 0x80 {
		// Single byte in [0x00, 0x7f]: encoded as itself.
		return []byte{b[0]}
	}
	if len(b) <= 55 {
		// Short string: 0x80 + len, then bytes.
		result := make([]byte, 1+len(b))
		result[0] = byte(0x80 + len(b))
		copy(result[1:], b)
		return result
	}
	// Long string: 0xb7 + length-of-length, then length bytes, then string.
	lenBytes := uint64ToMinBytes(uint64(len(b)))
	result := make([]byte, 1+len(lenBytes)+len(b))
	result[0] = byte(0xb7 + len(lenBytes))
	copy(result[1:], lenBytes)
	copy(result[1+len(lenBytes):], b)
	return result
}

// rlpPrependListHeader wraps payload bytes with a list header.
func rlpPrependListHeader(payload []byte) []byte {
	if len(payload) <= 55 {
		result := make([]byte, 1+len(payload))
		result[0] = byte(0xc0 + len(payload))
		copy(result[1:], payload)
		return result
	}
	lenBytes := uint64ToMinBytes(uint64(len(payload)))
	result := make([]byte, 1+len(lenBytes)+len(payload))
	result[0] = byte(0xf7 + len(lenBytes))
	copy(result[1:], lenBytes)
	copy(result[1+len(lenBytes):], payload)
	return result
}

// uint64ToMinBytes encodes a uint64 as big-endian bytes with no leading zeros.
func uint64ToMinBytes(v uint64) []byte {
	if v == 0 {
		return nil
	}
	// Find the number of significant bytes.
	var buf [8]byte
	buf[0] = byte(v >> 56)
	buf[1] = byte(v >> 48)
	buf[2] = byte(v >> 40)
	buf[3] = byte(v >> 32)
	buf[4] = byte(v >> 24)
	buf[5] = byte(v >> 16)
	buf[6] = byte(v >> 8)
	buf[7] = byte(v)
	for i := 0; i < 8; i++ {
		if buf[i] != 0 {
			return buf[i:]
		}
	}
	return buf[7:]
}
