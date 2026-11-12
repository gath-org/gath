package consensus

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

var errShort = io.ErrUnexpectedEOF

// W is a growing little-endian buffer.
type W struct {
	b []byte
}

func (w *W) Bytes() []byte { return w.b }

func (w *W) U8(v byte) { w.b = append(w.b, v) }

func (w *W) U16(v uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *W) U32(v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *W) U64(v uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	w.b = append(w.b, buf[:]...)
}

func (w *W) Raw(p []byte) { w.b = append(w.b, p...) }

func (w *W) Hash(h Hash) { w.b = append(w.b, h[:]...) }

// VarInt writes a Bitcoin-style compact size.
func (w *W) VarInt(n uint64) {
	switch {
	case n < 0xfd:
		w.U8(byte(n))
	case n <= math.MaxUint16:
		w.U8(0xfd)
		w.U16(uint16(n))
	case n <= math.MaxUint32:
		w.U8(0xfe)
		w.U32(uint32(n))
	default:
		w.U8(0xff)
		w.U64(n)
	}
}

// VarBytes writes a length-prefixed byte slice.
func (w *W) VarBytes(p []byte) {
	w.VarInt(uint64(len(p)))
	w.Raw(p)
}

// R reads little-endian values from a buffer.
type R struct {
	b []byte
}

func NewReader(b []byte) *R { return &R{b: b} }

func (r *R) Remaining() int { return len(r.b) }

func (r *R) Empty() error {
	if len(r.b) != 0 {
		return errors.New("trailing bytes")
	}
	return nil
}

func (r *R) take(n int) ([]byte, error) {
	if len(r.b) < n {
		return nil, errShort
	}
	p := r.b[:n]
	r.b = r.b[n:]
	return p, nil
}

func (r *R) U8() (byte, error) {
	p, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return p[0], nil
}

func (r *R) U16() (uint16, error) {
	p, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(p), nil
}

func (r *R) U32() (uint32, error) {
	p, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(p), nil
}

func (r *R) U64() (uint64, error) {
	p, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(p), nil
}

func (r *R) Hash() (Hash, error) {
	p, err := r.take(32)
	if err != nil {
		return Hash{}, err
	}
	var h Hash
	copy(h[:], p)
	return h, nil
}

func (r *R) Raw(n int) ([]byte, error) {
	p, err := r.take(n)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, p)
	return out, nil
}

// VarInt reads a canonical compact size.
func (r *R) VarInt() (uint64, error) {
	tag, err := r.U8()
	if err != nil {
		return 0, err
	}
	switch tag {
	case 0xff:
		v, err := r.U64()
		if err != nil {
			return 0, err
		}
		if v <= math.MaxUint32 {
			return 0, ErrNonMinimal
		}
		return v, nil
	case 0xfe:
		v, err := r.U32()
		if err != nil {
			return 0, err
		}
		if uint64(v) <= math.MaxUint16 {
			return 0, ErrNonMinimal
		}
		return uint64(v), nil
	case 0xfd:
		v, err := r.U16()
		if err != nil {
			return 0, err
		}
		if v < 0xfd {
			return 0, ErrNonMinimal
		}
		return uint64(v), nil
	default:
		return uint64(tag), nil
	}
}

// VarBytes reads a length-prefixed slice, rejecting lengths above max.
func (r *R) VarBytes(max int) ([]byte, error) {
	n, err := r.VarInt()
	if err != nil {
		return nil, err
	}
	if n > uint64(max) {
		return nil, errors.New("varbytes too large")
	}
	return r.Raw(int(n))
}
