package lsm

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTest(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(t.TempDir(), Options{MemtableBytes: 1 << 20, BloomBitsPerKey: 10, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}
func TestPutGetDelete(t *testing.T) {
	e := openTest(t)
	if err := e.Put([]byte("a"), []byte("one")); err != nil {
		t.Fatal(err)
	}
	v, err := e.Get([]byte("a"))
	if err != nil || string(v) != "one" {
		t.Fatalf("got %q, %v", v, err)
	}
	if err = e.Delete([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Get([]byte("a")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
func TestFlushRestartAndTombstone(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir, Options{MemtableBytes: 1 << 20, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Put([]byte("keep"), []byte("v1"))
	_ = e.Flush()
	_ = e.Put([]byte("keep"), []byte("v2"))
	_ = e.Put([]byte("gone"), []byte("x"))
	_ = e.Flush()
	_ = e.Delete([]byte("gone"))
	_ = e.Flush()
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = Open(dir, Options{SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	v, err := e.Get([]byte("keep"))
	if err != nil || string(v) != "v2" {
		t.Fatalf("got %q, %v", v, err)
	}
	if _, err = e.Get([]byte("gone")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tombstone lost: %v", err)
	}
}
func TestWALRecovery(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir, Options{SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Put([]byte("replay"), []byte("ok")); err != nil {
		t.Fatal(err)
	}
	_ = e.Close()
	e, err = Open(dir, Options{SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	v, err := e.Get([]byte("replay"))
	if err != nil || string(v) != "ok" {
		t.Fatalf("got %q, %v", v, err)
	}
}
func TestCompaction(t *testing.T) {
	e := openTest(t)
	_ = e.Put([]byte("a"), []byte("1"))
	_ = e.Flush()
	_ = e.Put([]byte("a"), []byte("2"))
	_ = e.Flush()
	if err := e.Compact(); err != nil {
		t.Fatal(err)
	}
	v, err := e.Get([]byte("a"))
	if err != nil || string(v) != "2" {
		t.Fatalf("got %q, %v", v, err)
	}
	if s := e.Stats(); s.SSTables != 1 {
		t.Fatalf("expected one table, got %d", s.SSTables)
	}
}
func TestHTTPAPI(t *testing.T) {
	e := openTest(t)
	h := e.Handler()
	req := httptest.NewRequest(http.MethodPut, "/v1/kv/alpha", strings.NewReader(`{"value":"beta"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("put status %d: %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/kv/alpha", nil))
	if rec.Code != 200 {
		t.Fatalf("get status %d", rec.Code)
	}
}
func TestFlushThreshold(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir, Options{MemtableBytes: 1, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Put([]byte("a"), []byte("b")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, fmt.Sprintf("table-%020d.sst", 0))); err != nil {
		t.Fatal(err)
	}
}
func BenchmarkPut(b *testing.B) {
	dir := b.TempDir()
	e, err := Open(dir, Options{MemtableBytes: 64 << 20, SyncWrites: false})
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.Put([]byte(fmt.Sprintf("key-%09d", i)), []byte("benchmark-value")); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkGet(b *testing.B) {
	e, err := Open(b.TempDir(), Options{MemtableBytes: 1 << 20, SyncWrites: false})
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	_ = e.Put([]byte("key"), []byte("value"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Get([]byte("key")); err != nil {
			b.Fatal(err)
		}
	}
}
