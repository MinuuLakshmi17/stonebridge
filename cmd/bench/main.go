package main

import (
	"fmt"
	"os"
	"time"

	"github.com/MinuuLakshmi17/stonebridge/internal/lsm"
)

func main() {
	dir, _ := os.MkdirTemp("", "bench")
	defer os.RemoveAll(dir)
	e, err := lsm.Open(dir, lsm.Options{MemtableBytes: 1 << 20, SyncWrites: false})
	if err != nil {
		panic(err)
	}
	const n = 50000
	t0 := time.Now()
	for i := 0; i < n; i++ {
		k := []byte(fmt.Sprintf("key-%08d", i))
		v := make([]byte, 100)
		for j := range v {
			v[j] = byte(i + j)
		}
		if err := e.Put(k, v); err != nil {
			panic(err)
		}
	}
	writeDur := time.Since(t0)
	t0 = time.Now()
	for i := 0; i < n; i++ {
		k := []byte(fmt.Sprintf("key-%08d", i))
		if _, err := e.Get(k); err != nil {
			panic(err)
		}
	}
	readDur := time.Since(t0)
	t0 = time.Now()
	const m = 10000
	for i := 0; i < m; i++ {
		k := []byte(fmt.Sprintf("nope-%08d", i))
		_, _ = e.Get(k)
	}
	missDur := time.Since(t0)
	st := e.Stats()
	e.Close()
	fmt.Printf("writes: %d in %v (%.0f writes/s)\n", n, writeDur.Round(time.Millisecond), float64(n)/writeDur.Seconds())
	fmt.Printf("reads:  %d in %v (%.0f reads/s)\n", n, readDur.Round(time.Millisecond), float64(n)/readDur.Seconds())
	fmt.Printf("misses: %d in %v (%.0f lookups/s)\n", m, missDur.Round(time.Millisecond), float64(m)/missDur.Seconds())
	fmt.Printf("sstable count: %d\n", st.SSTables)
}
