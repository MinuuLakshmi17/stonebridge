package lsm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var ErrNotFound = errors.New("key not found")

type entry struct {
	Key       []byte `json:"key"`
	Value     []byte `json:"value"`
	Tombstone bool   `json:"tombstone"`
	Seq       uint64 `json:"seq"`
}
type diskRecord struct {
	Key       []byte `json:"key"`
	Value     []byte `json:"value"`
	Tombstone bool   `json:"tombstone"`
	Seq       uint64 `json:"seq"`
}
type Options struct {
	MemtableBytes   int
	BloomBitsPerKey int
	SyncWrites      bool
}
type Stats struct {
	MemtableEntries int    `json:"memtable_entries"`
	MemtableBytes   int    `json:"memtable_bytes"`
	SSTables        int    `json:"sstables"`
	Keys            int    `json:"keys_approx"`
	WALBytes        int64  `json:"wal_bytes"`
	NextSequence    uint64 `json:"next_sequence"`
}
type Engine struct {
	mu        sync.RWMutex
	dir       string
	wal       *os.File
	opts      Options
	mem       map[string]entry
	memBytes  int
	tables    []*table
	nextSeq   uint64
	nextTable uint64
	closed    bool
}

func Open(dir string, opts Options) (*Engine, error) {
	if opts.MemtableBytes <= 0 {
		opts.MemtableBytes = 4 << 20
	}
	if opts.BloomBitsPerKey <= 0 {
		opts.BloomBitsPerKey = 10
	}
	if !opts.SyncWrites { /* false is supported intentionally */
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	e := &Engine{dir: dir, opts: opts, mem: make(map[string]entry), nextSeq: 1}
	if err := e.loadTables(); err != nil {
		return nil, err
	}
	walPath := filepath.Join(dir, "stonebridge.wal")
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	e.wal = f
	if err := e.replayWAL(); err != nil {
		f.Close()
		return nil, err
	}
	return e, nil
}
func (e *Engine) ensureOpen() error {
	if e.closed {
		return errors.New("engine closed")
	}
	return nil
}
func (e *Engine) Put(key, value []byte) error { return e.mutate(key, value, false) }
func (e *Engine) Delete(key []byte) error     { return e.mutate(key, nil, true) }
func (e *Engine) mutate(key, value []byte, tomb bool) error {
	if len(key) == 0 {
		return errors.New("key must not be empty")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureOpen(); err != nil {
		return err
	}
	r := entry{Key: append([]byte(nil), key...), Value: append([]byte(nil), value...), Tombstone: tomb, Seq: e.nextSeq}
	payload, err := json.Marshal(toDiskRecord(r))
	if err != nil {
		return err
	}
	var frame [8]byte
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(frame[4:], crc32.ChecksumIEEE(payload))
	if _, err = e.wal.Write(frame[:]); err != nil {
		return err
	}
	if _, err = e.wal.Write(payload); err != nil {
		return err
	}
	if e.opts.SyncWrites {
		if err = e.wal.Sync(); err != nil {
			return err
		}
	}
	e.nextSeq++
	e.putMem(r)
	if e.memBytes >= e.opts.MemtableBytes {
		return e.flushLocked()
	}
	return nil
}
func toDiskRecord(r entry) diskRecord {
	return diskRecord{Key: r.Key, Value: r.Value, Tombstone: r.Tombstone, Seq: r.Seq}
}
func (e *Engine) putMem(r entry) {
	k := string(r.Key)
	if old, ok := e.mem[k]; ok {
		e.memBytes -= len(old.Key) + len(old.Value) + 24
	}
	e.mem[k] = r
	e.memBytes += len(r.Key) + len(r.Value) + 24
}
func (e *Engine) Get(key []byte) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if err := e.ensureOpen(); err != nil {
		return nil, err
	}
	if r, ok := e.mem[string(key)]; ok {
		if r.Tombstone {
			return nil, ErrNotFound
		}
		return append([]byte(nil), r.Value...), nil
	}
	for _, t := range e.tables {
		if !t.mightContain(key) {
			continue
		}
		if r, ok := t.get(key); ok {
			if r.Tombstone {
				return nil, ErrNotFound
			}
			return append([]byte(nil), r.Value...), nil
		}
	}
	return nil, ErrNotFound
}
func (e *Engine) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureOpen(); err != nil {
		return err
	}
	return e.flushLocked()
}
func (e *Engine) flushLocked() error {
	if len(e.mem) == 0 {
		return nil
	}
	records := make([]entry, 0, len(e.mem))
	for _, r := range e.mem {
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return string(records[i].Key) < string(records[j].Key) })
	t, err := writeTable(e.dir, e.nextTable, records, e.opts.BloomBitsPerKey)
	if err != nil {
		return err
	}
	e.nextTable++
	e.tables = append([]*table{t}, e.tables...)
	e.mem = make(map[string]entry)
	e.memBytes = 0
	// WAL truncation is safe only because the just-flushed table is durable.
	if err = e.wal.Close(); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(e.dir, "stonebridge.wal"), nil, 0600); err != nil {
		return err
	}
	e.wal, err = os.OpenFile(filepath.Join(e.dir, "stonebridge.wal"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	return err
}
func (e *Engine) Compact() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensureOpen(); err != nil {
		return err
	}
	if len(e.mem) > 0 {
		if err := e.flushLocked(); err != nil {
			return err
		}
	}
	if len(e.tables) < 2 {
		return nil
	}
	latest := make(map[string]entry)
	for i := len(e.tables) - 1; i >= 0; i-- { // oldest to newest, newer overwrites
		for _, r := range e.tables[i].records {
			latest[string(r.Key)] = r
		}
	}
	records := make([]entry, 0, len(latest))
	for _, r := range latest {
		if !r.Tombstone {
			records = append(records, r)
		}
	}
	sort.Slice(records, func(i, j int) bool { return string(records[i].Key) < string(records[j].Key) })
	newID := e.nextTable
	t, err := writeTable(e.dir, newID, records, e.opts.BloomBitsPerKey)
	if err != nil {
		return err
	}
	e.nextTable++
	old := e.tables
	e.tables = []*table{t}
	for _, ot := range old {
		if ot.path != t.path {
			_ = os.Remove(ot.path)
		}
	}
	return nil
}
func (e *Engine) Stats() Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	st := Stats{MemtableEntries: len(e.mem), MemtableBytes: e.memBytes, SSTables: len(e.tables), NextSequence: e.nextSeq}
	for _, t := range e.tables {
		st.Keys += len(t.records)
	}
	if fi, err := os.Stat(filepath.Join(e.dir, "stonebridge.wal")); err == nil {
		st.WALBytes = fi.Size()
	}
	return st
}
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	if err := e.wal.Sync(); err != nil {
		return err
	}
	if err := e.wal.Close(); err != nil {
		return err
	}
	e.closed = true
	return nil
}
func (e *Engine) loadTables() error {
	matches, err := filepath.Glob(filepath.Join(e.dir, "table-*.sst"))
	if err != nil {
		return err
	}
	type item struct {
		id   uint64
		path string
	}
	items := make([]item, 0, len(matches))
	for _, p := range matches {
		base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "table-"), ".sst")
		id, err := strconv.ParseUint(base, 10, 64)
		if err == nil {
			items = append(items, item{id, p})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id > items[j].id })
	for _, it := range items {
		t, err := readTable(it.path, e.opts.BloomBitsPerKey)
		if err != nil {
			return fmt.Errorf("load %s: %w", it.path, err)
		}
		e.tables = append(e.tables, t)
		if it.id >= e.nextTable {
			e.nextTable = it.id + 1
		}
		for _, r := range t.records {
			if r.Seq >= e.nextSeq {
				e.nextSeq = r.Seq + 1
			}
		}
	}
	return nil
}
func (e *Engine) replayWAL() error {
	if _, err := e.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	for {
		var hdr [8]byte
		_, err := io.ReadFull(e.wal, hdr[:])
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:4])
		if n == 0 || n > 64<<20 {
			break
		}
		payload := make([]byte, n)
		if _, err = io.ReadFull(e.wal, payload); err != nil {
			break
		}
		if crc32.ChecksumIEEE(payload) != binary.BigEndian.Uint32(hdr[4:]) {
			break
		}
		var r diskRecord
		if err = json.Unmarshal(payload, &r); err != nil {
			break
		}
		// A flushed table may contain the same sequence; only replay newer records.
		found := false
		for _, t := range e.tables {
			if tr, ok := t.get(r.Key); ok && tr.Seq >= r.Seq {
				found = true
				break
			}
		}
		if !found {
			e.putMem(entry(r))
		}
		if r.Seq >= e.nextSeq {
			e.nextSeq = r.Seq + 1
		}
	}
	_, err := e.wal.Seek(0, io.SeekEnd)
	return err
}
