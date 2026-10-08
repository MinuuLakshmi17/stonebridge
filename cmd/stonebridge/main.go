package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MinuuLakshmi17/stonebridge/internal/lsm"
)

func main() {
	dir := flag.String("dir", "./stonebridge-data", "database directory")
	addr := flag.String("http", ":8080", "HTTP listen address")
	mem := flag.Int("memtable-bytes", 4<<20, "memtable flush threshold")
	syncWrites := flag.Bool("sync-writes", true, "fsync WAL after each mutation")
	flag.Parse()
	db, err := lsm.Open(*dir, lsm.Options{MemtableBytes: *mem, BloomBitsPerKey: 10, SyncWrites: *syncWrites})
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: *addr, Handler: db.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("Stonebridge listening on %s (data=%s)", *addr, *dir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if err := db.Close(); err != nil {
		log.Printf("close: %v", err)
	}
}
