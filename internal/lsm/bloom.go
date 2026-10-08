package lsm

import "hash/fnv"

type bloom struct {
	bits []uint64
	k    uint32
}

func newBloom(n, bitsPerKey int) *bloom {
	if bitsPerKey < 1 {
		bitsPerKey = 10
	}
	m := n * bitsPerKey
	if m < 64 {
		m = 64
	}
	return &bloom{bits: make([]uint64, (m+63)/64), k: 7}
}
func bloomHashes(key []byte) (uint64, uint64) {
	h := fnv.New64a()
	_, _ = h.Write(key)
	a := h.Sum64()
	h.Reset()
	_, _ = h.Write([]byte{0x9d, 0x37})
	_, _ = h.Write(key)
	b := h.Sum64()
	if b == 0 {
		b = 0x9e3779b97f4a7c15
	}
	return a, b
}
func (b *bloom) add(key []byte) {
	a, c := bloomHashes(key)
	m := uint64(len(b.bits) * 64)
	for i := uint32(0); i < b.k; i++ {
		p := (a + uint64(i)*c) % m
		b.bits[p/64] |= 1 << (p % 64)
	}
}
func (b *bloom) mightContain(key []byte) bool {
	a, c := bloomHashes(key)
	m := uint64(len(b.bits) * 64)
	for i := uint32(0); i < b.k; i++ {
		p := (a + uint64(i)*c) % m
		if b.bits[p/64]&(1<<(p%64)) == 0 {
			return false
		}
	}
	return true
}
