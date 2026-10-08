package lsm

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

var tableMagic = []byte("STONE001")

type table struct {
	path    string
	records []entry
	bloom   *bloom
	sparse  []string // every 16th key, in sorted order; narrows binary search
}

func writeTable(dir string, id uint64, records []entry, bitsPerKey int) (*table, error) {
	sort.Slice(records, func(i, j int) bool { return string(records[i].Key) < string(records[j].Key) })
	path := filepath.Join(dir, formatTableName(id))
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(tableMagic); err != nil {
		return nil, err
	}
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(records)))
	if _, err = f.Write(count[:]); err != nil {
		return nil, err
	}
	for _, r := range records {
		if len(r.Key) > 1<<24 || len(r.Value) > 1<<30 {
			return nil, errors.New("record too large")
		}
		var hdr [17]byte
		binary.BigEndian.PutUint32(hdr[0:4], uint32(len(r.Key)))
		binary.BigEndian.PutUint32(hdr[4:8], uint32(len(r.Value)))
		if r.Tombstone {
			hdr[8] = 1
		}
		binary.BigEndian.PutUint64(hdr[9:17], r.Seq)
		if _, err = f.Write(hdr[:]); err != nil {
			return nil, err
		}
		if _, err = f.Write(r.Key); err != nil {
			return nil, err
		}
		if _, err = f.Write(r.Value); err != nil {
			return nil, err
		}
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Rename(tmp, path); err != nil {
		return nil, err
	}
	ok = true
	return buildTable(path, records, bitsPerKey), nil
}
func readTable(path string, bitsPerKey int) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	magic := make([]byte, 8)
	if _, err = io.ReadFull(f, magic); err != nil {
		return nil, err
	}
	if string(magic) != string(tableMagic) {
		return nil, errors.New("invalid SSTable magic")
	}
	var c [4]byte
	if _, err = io.ReadFull(f, c[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(c[:])
	if n > 100_000_000 {
		return nil, errors.New("unreasonable SSTable record count")
	}
	rs := make([]entry, 0, n)
	for i := uint32(0); i < n; i++ {
		var h [17]byte
		if _, err = io.ReadFull(f, h[:]); err != nil {
			return nil, err
		}
		kl := binary.BigEndian.Uint32(h[0:4])
		vl := binary.BigEndian.Uint32(h[4:8])
		if kl == 0 || kl > 1<<24 || vl > 1<<30 {
			return nil, errors.New("invalid SSTable record lengths")
		}
		k := make([]byte, kl)
		v := make([]byte, vl)
		if _, err = io.ReadFull(f, k); err != nil {
			return nil, err
		}
		if _, err = io.ReadFull(f, v); err != nil {
			return nil, err
		}
		rs = append(rs, entry{Key: k, Value: v, Tombstone: h[8] != 0, Seq: binary.BigEndian.Uint64(h[9:17])})
	}
	return buildTable(path, rs, bitsPerKey), nil
}
func buildTable(path string, rs []entry, bitsPerKey int) *table {
	t := &table{path: path, records: rs, bloom: newBloom(len(rs), bitsPerKey)}
	for i, r := range rs {
		t.bloom.add(r.Key)
		if i%16 == 0 {
			t.sparse = append(t.sparse, string(r.Key))
		}
	}
	return t
}
func (t *table) mightContain(k []byte) bool { return t.bloom.mightContain(k) }
func (t *table) get(k []byte) (entry, bool) {
	n := len(t.records)
	if n == 0 {
		return entry{}, false
	}
	ks := string(k)
	// Find the greatest sampled key <= k; the target can only live in the
	// 16-record window starting at that sample.
	lo := sort.Search(len(t.sparse), func(i int) bool { return t.sparse[i] > ks })
	start := 0
	if lo > 0 {
		start = (lo - 1) * 16
	}
	end := start + 16
	if end > n {
		end = n
	}
	i := sort.Search(end-start, func(j int) bool { return string(t.records[start+j].Key) >= ks })
	if idx := start + i; idx < end && string(t.records[idx].Key) == ks {
		return t.records[idx], true
	}
	return entry{}, false
}
func formatTableName(id uint64) string {
	return "table-" + pad20(id) + ".sst"
}
func pad20(id uint64) string {
	s := strconv.FormatUint(id, 10)
	for len(s) < 20 {
		s = "0" + s
	}
	return s
}
