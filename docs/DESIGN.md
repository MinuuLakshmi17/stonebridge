# Design notes

## Write path

`Put`/`Delete` acquire the engine write lock, append a JSON-encoded mutation framed by length and CRC32 to the WAL, sync the WAL, then update the memtable. A successful return means the WAL sync completed. This favors clarity and durability over peak write throughput.

## Read path

The memtable wins over disk. SSTables are checked newest-first. Each table has a Bloom filter (to avoid definite-miss scans) and a sparse index (to narrow the sorted scan). A tombstone is a real newest value and therefore prevents older values from resurfacing.

## SSTable format

Header: 8-byte magic `STONE001`, followed by a 32-bit record count. Each sorted record contains a 32-bit key length, 32-bit value length, one byte tombstone flag, then key and value bytes. The entire file is loaded at open into an in-memory table representation in this educational version. This makes the format simple to inspect, but does not provide bounded-memory reads like a production block cache.

## Compaction

Manual compaction merges all tables newest-first and keeps the newest record for each key. Since this prototype compacts *all* discovered tables at once, tombstones can be dropped safely after older versions have been eliminated. The result is atomically renamed into a new table file before old files are removed.

## Limitations and next steps

- No concurrent process lock, manifest, WAL rotation, background compaction, MVCC, range scans, compression, or block cache.
- Tables are loaded into memory on startup; Bloom filters and sparse indexes are in-memory only.
- HTTP API has no authentication/TLS; keep it on trusted networks.
- Crash testing is not exhaustive; rename and directory-sync semantics vary by filesystem.
- Compaction is synchronous and holds the engine lock.
- For production quality, introduce manifest-driven file lifecycle, table checksums, WAL segments, bounded block cache, leveled size budgets, compaction scheduling, and property/fault-injection testing.
