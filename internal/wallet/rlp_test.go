package wallet

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
)

func TestRLPEncodeBytes_Empty(t *testing.T) {
	// Empty string -> 0x80
	got := rlpEncodeBytes(nil)
	want := []byte{0x80}
	if !bytes.Equal(got, want) {
		t.Errorf("empty bytes: got %x, want %x", got, want)
	}
}

func TestRLPEncodeBytes_SingleLow(t *testing.T) {
	// Single byte < 0x80 -> itself
	got := rlpEncodeBytes([]byte{0x42})
	want := []byte{0x42}
	if !bytes.Equal(got, want) {
		t.Errorf("single low byte: got %x, want %x", got, want)
	}
}

func TestRLPEncodeBytes_SingleZero(t *testing.T) {
	// Single byte 0x00 -> itself (not 0x80)
	got := rlpEncodeBytes([]byte{0x00})
	want := []byte{0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("single zero byte: got %x, want %x", got, want)
	}
}

func TestRLPEncodeBytes_SingleHigh(t *testing.T) {
	// Single byte >= 0x80 -> 0x81, byte
	got := rlpEncodeBytes([]byte{0x80})
	want := []byte{0x81, 0x80}
	if !bytes.Equal(got, want) {
		t.Errorf("single high byte: got %x, want %x", got, want)
	}
}

func TestRLPEncodeBytes_ShortString(t *testing.T) {
	// "dog" = [0x64, 0x6f, 0x67] -> 0x83, 0x64, 0x6f, 0x67
	got := rlpEncodeBytes([]byte("dog"))
	want := []byte{0x83, 0x64, 0x6f, 0x67}
	if !bytes.Equal(got, want) {
		t.Errorf("short string 'dog': got %x, want %x", got, want)
	}
}

func TestRLPEncodeList_Empty(t *testing.T) {
	// Empty list -> 0xc0
	got := rlpEncodeList([]any{})
	want := []byte{0xc0}
	if !bytes.Equal(got, want) {
		t.Errorf("empty list: got %x, want %x", got, want)
	}
}

func TestRLPEncodeList_Strings(t *testing.T) {
	// ["cat", "dog"]
	got := rlpEncodeList([]any{[]byte("cat"), []byte("dog")})
	want := []byte{0xc8, 0x83, 'c', 'a', 't', 0x83, 'd', 'o', 'g'}
	if !bytes.Equal(got, want) {
		t.Errorf("list [cat, dog]: got %x, want %x", got, want)
	}
}

func TestRLPEncodeItem_BigInt(t *testing.T) {
	tests := []struct {
		name string
		val  *big.Int
		want string
	}{
		{"zero", big.NewInt(0), "80"},
		{"one", big.NewInt(1), "01"},
		{"127", big.NewInt(127), "7f"},
		{"128", big.NewInt(128), "8180"},
		{"256", big.NewInt(256), "820100"},
		{"1024", big.NewInt(1024), "820400"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hex.EncodeToString(rlpEncodeItem(tt.val))
			if got != tt.want {
				t.Errorf("big.Int %s: got %s, want %s", tt.name, got, tt.want)
			}
		})
	}
}

func TestRLPEncodeItem_Uint64(t *testing.T) {
	tests := []struct {
		name string
		val  uint64
		want string
	}{
		{"zero", 0, "80"},
		{"one", 1, "01"},
		{"127", 127, "7f"},
		{"128", 128, "8180"},
		{"21000", 21000, "825208"},
		{"1000000000", 1000000000, "843b9aca00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hex.EncodeToString(rlpEncodeItem(tt.val))
			if got != tt.want {
				t.Errorf("uint64 %s: got %s, want %s", tt.name, got, tt.want)
			}
		})
	}
}

func TestRLPEncodeList_Nested(t *testing.T) {
	// [[], [[]], [[], [[]]]]
	// This is a well-known RLP test vector.
	got := rlpEncodeList([]any{
		[]any{},
		[]any{[]any{}},
		[]any{[]any{}, []any{[]any{}}},
	})
	want, _ := hex.DecodeString("c7c0c1c0c3c0c1c0")
	if !bytes.Equal(got, want) {
		t.Errorf("nested empty lists: got %x, want %x", got, want)
	}
}

func TestUint64ToMinBytes(t *testing.T) {
	tests := []struct {
		val  uint64
		want string
	}{
		{0, ""},
		{1, "01"},
		{255, "ff"},
		{256, "0100"},
		{21000, "5208"},
		{0xdeadbeef, "deadbeef"},
	}
	for _, tt := range tests {
		got := hex.EncodeToString(uint64ToMinBytes(tt.val))
		if got != tt.want {
			t.Errorf("uint64ToMinBytes(%d): got %s, want %s", tt.val, got, tt.want)
		}
	}
}

func TestRLPEncodeBytes_LongString(t *testing.T) {
	// 56-byte string (triggers long string encoding)
	data := make([]byte, 56)
	for i := range data {
		data[i] = byte(i)
	}
	got := rlpEncodeBytes(data)
	// 0xb8 (0xb7 + 1 byte for length), 0x38 (56), then data
	if got[0] != 0xb8 {
		t.Errorf("long string prefix: got %x, want b8", got[0])
	}
	if got[1] != 56 {
		t.Errorf("long string length: got %d, want 56", got[1])
	}
	if !bytes.Equal(got[2:], data) {
		t.Error("long string payload mismatch")
	}
}
