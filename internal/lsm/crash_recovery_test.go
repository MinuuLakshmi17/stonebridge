package lsm

import (
	"fmt"
	"testing"
)

// Crash recovery: writes flushed to SSTable plus unflushed WAL writes must
// all survive a reopen without Close (simulated crash).
func TestCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(dir, Options{MemtableBytes: 1 << 20, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := e.Put([]byte(fmt.Sprintf("key-%03d", i)), []byte(fmt.Sprintf("val-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 150; i++ {
		if err := e.Put([]byte(fmt.Sprintf("key-%03d", i)), []byte(fmt.Sprintf("val-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	// NOTE: no Close — simulates a crash.

	e2, err := Open(dir, Options{MemtableBytes: 1 << 20, SyncWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	for i := 0; i < 150; i++ {
		v, err := e2.Get([]byte(fmt.Sprintf("key-%03d", i)))
		if err != nil || string(v) != fmt.Sprintf("val-%d", i) {
			t.Fatalf("key-%03d: got %q, %v", i, v, err)
		}
	}
	if err := e2.Put([]byte("key-000"), []byte("updated")); err != nil {
		t.Fatal(err)
	}
	if err := e2.Delete([]byte("key-001")); err != nil {
		t.Fatal(err)
	}
	if err := e2.Compact(); err != nil {
		t.Fatal(err)
	}
	if v, _ := e2.Get([]byte("key-000")); string(v) != "updated" {
		t.Fatalf("overwrite after compact failed: %q", v)
	}
	if _, err := e2.Get([]byte("key-001")); err == nil {
		t.Fatal("deleted key still visible after compact")
	}
	if st := e2.Stats(); st.SSTables != 1 {
		t.Fatalf("expected 1 table after compact, got %d", st.SSTables)
	}
}
