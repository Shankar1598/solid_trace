// Package pebblevalsep is a throwaway benchmark for
// .scratch/upgrade-pebble-duckdb/issues/03: should EventStore turn on Pebble
// value separation for Event payloads? Results are in
// .scratch/upgrade-pebble-duckdb/research/pebble-value-separation.md.
//
// Every test skips unless PEBBLE_VALSEP_BENCH=1. PEBBLE_VALSEP_POLICY picks the
// policy: off (the default), on (MinimumSize 512), on2k (MinimumSize 2048) or
// onz (on, with zstd blob files).
// PEBBLE_VALSEP_MB sets how much Event data TestWrite writes (default 1024).
// PEBBLE_VALSEP_LBASE_MB replaces LBaseMaxBytes's 64 MB, to give the store the
// levels of a larger one.
//
//	PEBBLE_VALSEP_BENCH=1 PEBBLE_VALSEP_POLICY=on go test -p 1 -count=1 -v -run 'TestWrite$' ./bench/pebblevalsep/
//
// Run one test at a time, in the order TestWrite, TestGet, TestCompact; each
// takes under a minute. TestWrite leaves its store in
// ~/.cache/solid-trace/pebble-valsep/<policy>[-lbase<MB>] for the other two.
// TestTiering stands alone. TestClean deletes every store.
package pebblevalsep

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/objstorage/remote"
	"github.com/cockroachdb/pebble/v2/sstable"
	"github.com/cockroachdb/pebble/v2/vfs"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// policies are the value separation settings compared. "on" is what Pebble's
// own benchmark tool uses (cmd/pebble/db.go); every generated Event is over
// its 512 B threshold. "on2k" separates only Events of 2 KB or more. "onz" is
// "on" with zstd for blob files written into L0–L4 (see zstdBlobs).
var policies = map[string]func() pebble.ValueSeparationPolicy{
	"off":  nil,
	"on":   separateFrom(512),
	"on2k": separateFrom(2048),
	"onz":  separateFrom(512),
}

func separateFrom(minimumSize int) func() pebble.ValueSeparationPolicy {
	return func() pebble.ValueSeparationPolicy {
		return pebble.ValueSeparationPolicy{
			Enabled: true, MinimumSize: minimumSize, MaxBlobReferenceDepth: 10,
			RewriteMinimumAge: 5 * time.Minute, TargetGarbageRatio: 1.0,
		}
	}
}

// zstdBlobs gives L0–L4 the Balanced compression profile, as
// DBCompressionBalanced gives L6, so blob files are zstd, not MinLZ. A blob
// file is compressed for the level its flush or compaction writes, and
// Events' blob files are written by flushes, into L0. The Fast profile (L5's)
// is not enough: its adaptive compressor keeps MinLZ unless zstd saves a
// further 30%, and Event JSON in 4 KB blocks doesn't.
func zstdBlobs(o *pebble.Options) {
	for i := range 5 {
		o.Levels[i].Compression = func() *sstable.CompressionProfile { return sstable.BalancedCompression }
	}
}

const (
	// payloadCount distinct payloads are generated; each write copies one
	// and gives it its own event_id.
	payloadCount = 8192
	// writers is how many goroutines write, like concurrent ingest requests.
	writers = 4
	keyLen  = 20
)

// projectWeights spreads Events over four Projects, one much busier than
// the rest, so Event keys grow at four places in the keyspace.
var projectWeights = []int{55, 25, 15, 5}

type env struct {
	root, policy string
	// lbaseMB, when set, replaces LBaseMaxBytes's 64 MB, so a small store
	// has the levels of a larger one.
	lbaseMB int
}

func envOrSkip(t *testing.T) env {
	t.Helper()
	if os.Getenv("PEBBLE_VALSEP_BENCH") == "" {
		t.Skip("set PEBBLE_VALSEP_BENCH=1 to run")
	}
	e := env{policy: os.Getenv("PEBBLE_VALSEP_POLICY")}
	if e.policy == "" {
		e.policy = "off"
	}
	if _, ok := policies[e.policy]; !ok {
		t.Fatalf("unknown PEBBLE_VALSEP_POLICY %q", e.policy)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	e.root = filepath.Join(home, ".cache/solid-trace/pebble-valsep")
	if v := os.Getenv("PEBBLE_VALSEP_LBASE_MB"); v != "" {
		if e.lbaseMB, err = strconv.Atoi(v); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// name names the store: the policy, and the LBaseMaxBytes if not 64 MB.
func (e env) name() string {
	if e.lbaseMB != 0 {
		return fmt.Sprintf("%s-lbase%d", e.policy, e.lbaseMB)
	}
	return e.policy
}

func (e env) storeDir() string { return filepath.Join(e.root, e.name()) }
func (e env) keysFile() string { return filepath.Join(e.root, e.name()+".keys") }
func (e env) userFile() string { return filepath.Join(e.root, e.name()+".user") }

// open opens the store with the policy.
func (e env) open(t *testing.T, ev *events) *pebble.DB {
	t.Helper()
	return openStore(t, e.storeDir(), e.policy, ev, func(o *pebble.Options) {
		if e.lbaseMB != 0 {
			o.LBaseMaxBytes = int64(e.lbaseMB) << 20
		}
	})
}

// written returns TestWrite's keys and its bytes of keys and values.
func (e env) written(t *testing.T) ([]byte, float64) {
	t.Helper()
	keys, err := os.ReadFile(e.keysFile())
	if err != nil {
		t.Fatalf("run TestWrite first: %v", err)
	}
	user, err := os.ReadFile(e.userFile())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseFloat(string(user), 64)
	if err != nil {
		t.Fatal(err)
	}
	return keys, n
}

// events records what Pebble's EventListener and Logger report.
type events struct {
	mu         sync.Mutex
	stalls     int
	stallStart time.Time
	stalled    time.Duration
	blobPaths  []string
	messages   []string
}

func (ev *events) listener() *pebble.EventListener {
	return &pebble.EventListener{
		BackgroundError: func(err error) { ev.note("background error: %v", err) },
		BlobFileCreated: func(info pebble.BlobFileCreateInfo) {
			ev.mu.Lock()
			ev.blobPaths = append(ev.blobPaths, info.Reason+" "+info.Path)
			ev.mu.Unlock()
		},
		WriteStallBegin: func(pebble.WriteStallBeginInfo) {
			ev.mu.Lock()
			ev.stalls++
			ev.stallStart = time.Now()
			ev.mu.Unlock()
		},
		WriteStallEnd: func() {
			ev.mu.Lock()
			ev.stalled += time.Since(ev.stallStart)
			ev.mu.Unlock()
		},
		PossibleAPIMisuse: func(info pebble.PossibleAPIMisuseInfo) { ev.note("possible API misuse: %s", info) },
	}
}

func (ev *events) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "pebble:", msg)
	ev.mu.Lock()
	ev.messages = append(ev.messages, msg)
	ev.mu.Unlock()
}

// Infof, Errorf and Fatalf make events Pebble's Logger.
func (ev *events) Infof(format string, args ...any) {
	ev.mu.Lock()
	ev.messages = append(ev.messages, "info: "+fmt.Sprintf(format, args...))
	ev.mu.Unlock()
}
func (ev *events) Errorf(format string, args ...any) { ev.note("error: "+format, args...) }
func (ev *events) Fatalf(format string, args ...any) {
	ev.note("fatal: "+format, args...)
	panic(fmt.Sprintf(format, args...))
}

// openStore opens Pebble with storage/pebble.go's options and the policy.
func openStore(t *testing.T, dir, policy string, ev *events, tweak func(*pebble.Options)) *pebble.DB {
	t.Helper()
	opts := &pebble.Options{
		FormatMajorVersion:    pebble.FormatValueSeparation,
		MemTableSize:          64 << 20,
		LBaseMaxBytes:         64 << 20,
		L0CompactionThreshold: 4,
		L0StopWritesThreshold: 12,
		Logger:                ev,
		EventListener:         ev.listener(),
	}
	opts.ApplyCompressionSettings(func() pebble.DBCompressionSettings { return pebble.DBCompressionBalanced })
	opts.Experimental.ValueSeparationPolicy = policies[policy]
	if policy == "onz" {
		zstdBlobs(opts)
	}
	if tweak != nil {
		tweak(opts)
	}
	db, err := pebble.Open(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type writeResult struct {
	events    int
	userBytes int64
	elapsed   time.Duration
	// keys holds every Event key, keyLen bytes each, in commit order; sums
	// the CRC-32 of each one's value.
	keys    []byte
	sums    []uint32
	latency []time.Duration
}

// writeEvents writes Events until target bytes of keys and values are
// written, the way PebbleWriter.WriteEvent does: one batch per Event, a
// UUIDv7 key generated under a mutex, committed with NoSync.
func writeEvents(db *pebble.DB, payloads [][]byte, target int64) writeResult {
	var (
		mu      sync.Mutex
		res     writeResult
		written atomic.Int64
		wg      sync.WaitGroup
		lats    = make([][]time.Duration, writers)
	)
	var totalWeight int
	for _, w := range projectWeights {
		totalWeight += w
	}
	start := time.Now()
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(w), 42))
			buf := make([]byte, 0, 8<<10)
			var id [16]byte
			for written.Load() < target {
				p := payloads[r.IntN(len(payloads))]
				buf = append(buf[:0], p...)
				binary.LittleEndian.PutUint64(id[:8], r.Uint64())
				binary.LittleEndian.PutUint64(id[8:], r.Uint64())
				hex.Encode(buf[eventIDOffset:eventIDOffset+32], id[:])
				project, x := uint32(1), r.IntN(totalWeight)
				for _, weight := range projectWeights {
					if x < weight {
						break
					}
					x -= weight
					project++
				}
				sum := crc32.ChecksumIEEE(buf)

				began := time.Now()
				mu.Lock()
				eventUUID, err := uuid.NewV7()
				if err != nil {
					panic(err)
				}
				key := binary.BigEndian.AppendUint32(make([]byte, 0, keyLen), project)
				key = append(key, eventUUID[:]...)
				batch := db.NewBatch()
				if err := batch.Set(key, buf, nil); err != nil {
					panic(err)
				}
				if err := batch.Commit(pebble.NoSync); err != nil {
					panic(err)
				}
				batch.Close()
				res.keys = append(res.keys, key...)
				res.sums = append(res.sums, sum)
				mu.Unlock()
				lats[w] = append(lats[w], time.Since(began))
				written.Add(int64(len(key) + len(buf)))
			}
		}()
	}
	wg.Wait()
	res.elapsed = time.Since(start)
	res.events = len(res.sums)
	res.userBytes = written.Load()
	for _, l := range lats {
		res.latency = append(res.latency, l...)
	}
	return res
}

// settle flushes the memtable and waits until no flush or compaction has
// run for two seconds. It doesn't wait for Compact.EstimatedDebt to reach
// zero: Event keys only grow, so L0's files don't overlap, L0's score stays
// under 1 however many files it holds, and Pebble leaves them there.
func settle(t *testing.T, db *pebble.DB, timeout time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	if err := db.Flush(); err != nil {
		t.Fatal(err)
	}
	var lastCount int64 = -1
	var quietSince time.Time
	for time.Since(start) < timeout {
		m := db.Metrics()
		busy := m.Compact.NumInProgress > 0 || m.Flush.NumInProgress > 0
		if busy || m.Compact.Count != lastCount {
			lastCount, quietSince = m.Compact.Count, time.Now()
		} else if time.Since(quietSince) >= 2*time.Second {
			return time.Since(start) - 2*time.Second
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("compactions had not settled after %s", timeout)
	return time.Since(start)
}

// amp is the write amplification and space Metrics report.
type amp struct {
	wal, flushed, compacted, tables, blobs uint64
}

// measure reads amp from Metrics. Pebble v2.1.7 counts a flush's blob files
// in both BlobBytesFlushed and BlobBytesCompacted (compaction.go:3451-3454),
// so the flushed bytes are taken out of the compacted ones.
func measure(m *pebble.Metrics) amp {
	a := amp{wal: m.WAL.BytesWritten, blobs: m.BlobFiles.LiveSize}
	for _, l := range m.Levels {
		a.flushed += l.TableBytesFlushed + l.BlobBytesFlushed
		a.compacted += l.TableBytesCompacted + l.BlobBytesCompacted - l.BlobBytesFlushed
		a.tables += uint64(l.TablesSize)
	}
	return a
}

func logLSM(t *testing.T, m *pebble.Metrics) {
	t.Helper()
	for i, l := range m.Levels {
		if l.TablesCount == 0 && l.TableBytesCompacted == 0 && l.TableBytesFlushed == 0 && l.TableBytesMoved == 0 {
			continue
		}
		t.Logf("  L%d: %d tables %s; in %s, flushed %s+%s blob, compacted %s+%s blob, moved %s",
			i, l.TablesCount, mb(uint64(l.TablesSize)), mb(l.TableBytesIn), mb(l.TableBytesFlushed), mb(l.BlobBytesFlushed),
			mb(l.TableBytesCompacted), mb(l.BlobBytesCompacted-l.BlobBytesFlushed), mb(l.TableBytesMoved))
	}
	c := m.Compact
	t.Logf("  compactions: %d (default %d, move %d, copy %d, multi-level %d, blob rewrite %d)",
		c.Count, c.DefaultCount, c.MoveCount, c.CopyCount, c.MultiLevelCount, c.BlobFileRewriteCount)
	b := m.BlobFiles
	t.Logf("  blob files: %d live (%d local), %s (%s local); values %s, referenced %s",
		b.LiveCount, b.Local.LiveCount, mb(b.LiveSize), mb(b.Local.LiveSize), mb(b.ValueSize), mb(b.ReferencedValueSize))
}

func mb(n uint64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return s[int(p*float64(len(s)-1))]
}

func latencies(d []time.Duration) string {
	return fmt.Sprintf("p50 %s, p99 %s, max %s", percentile(d, 0.5), percentile(d, 0.99), percentile(d, 1))
}

// TestWrite writes PEBBLE_VALSEP_MB of Events at saturation into a new
// store, waits for compactions to settle, and reports write amplification,
// space and throughput. It keeps the store and its keys for TestGet.
func TestWrite(t *testing.T) {
	e := envOrSkip(t)
	targetMB := 1024
	if v := os.Getenv("PEBBLE_VALSEP_MB"); v != "" {
		var err error
		if targetMB, err = strconv.Atoi(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(e.storeDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.root, 0o755); err != nil {
		t.Fatal(err)
	}
	payloads := newPayloads(payloadCount, 1)
	ev := &events{}
	db := e.open(t, ev)

	res := writeEvents(db, payloads, int64(targetMB)<<20)
	atEnd := measure(db.Metrics())
	settled := settle(t, db, 45*time.Second)
	m := db.Metrics()
	a := measure(m)

	t.Logf("store %s: %d Events, %s of keys and values, written in %s", e.name(), res.events, mb(uint64(res.userBytes)), res.elapsed.Round(time.Millisecond))
	t.Logf("throughput: %.0f Events/s, %.1f MB/s; write stalls %d, %s stalled; commit latency %s",
		float64(res.events)/res.elapsed.Seconds(), float64(res.userBytes)/(1<<20)/res.elapsed.Seconds(),
		ev.stalls, ev.stalled.Round(time.Millisecond), latencies(res.latency))
	t.Logf("compaction during writes: flushed %s, compacted %s; settled %s later", mb(atEnd.flushed), mb(atEnd.compacted), settled.Round(time.Millisecond))
	user := float64(res.userBytes)
	t.Logf("write amp: flushed %s + compacted %s = %.2fx of user data (%.2fx with the WAL's %s)",
		mb(a.flushed), mb(a.compacted), float64(a.flushed+a.compacted)/user, float64(a.wal+a.flushed+a.compacted)/user, mb(a.wal))
	t.Logf("space: sst %s + blob %s = %s (%.2f bytes per user byte)", mb(a.tables), mb(a.blobs), mb(a.tables+a.blobs), float64(a.tables+a.blobs)/user)
	logLSM(t, m)
	if testing.Verbose() {
		t.Logf("metrics:\n%s", m)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.keysFile(), res.keys, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.userFile(), []byte(strconv.FormatInt(res.userBytes, 10)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dropPageCache asks the kernel to drop every file of dir from the page
// cache, so the next reads go to disk.
func dropPageCache(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// warmPageCache reads every file of dir once.
func warmPageCache(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(io.Discard, f)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// timeGets times n Gets of keys picked at random from keys[lo:hi).
func timeGets(t *testing.T, db *pebble.DB, keys []byte, lo, hi, n int, seed uint64) []time.Duration {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, 3))
	out := make([]time.Duration, n)
	for i := range out {
		k := lo + r.IntN(hi-lo)
		key := keys[k*keyLen : (k+1)*keyLen]
		began := time.Now()
		v, closer, err := db.Get(key)
		if err != nil {
			t.Fatalf("get %x: %v", key, err)
		}
		_ = v[len(v)-1]
		closer.Close()
		out[i] = time.Since(began)
	}
	return out
}

// TestGet times point reads of TestWrite's store: recent and old Events,
// cold (store reopened, its files dropped from the page cache) and warm
// (files in the page cache, the default 8 MB block cache). It also times a
// scan like EventsAfter's.
func TestGet(t *testing.T) {
	e := envOrSkip(t)
	keys, _ := e.written(t)
	n := len(keys) / keyLen
	// Recent is the newest 5% of Events; old is the oldest half.
	recentLo, oldHi := n-n/20, n/2
	ev := &events{}

	for _, c := range []struct {
		name   string
		lo, hi int
	}{{"recent", recentLo, n}, {"old", 0, oldHi}} {
		dropPageCache(t, e.storeDir())
		db := e.open(t, ev)
		d := timeGets(t, db, keys, c.lo, c.hi, 1000, 1)
		t.Logf("cold %s Get: %s", c.name, latencies(d))
		db.Close()
	}

	warmPageCache(t, e.storeDir())
	db := e.open(t, ev)
	defer db.Close()
	// Recent: the newest 2,000 Events, read once first, so most of their
	// blocks are in the block cache.
	timeGets(t, db, keys, n-2000, n, 4000, 2)
	t.Logf("warm hot Get (newest 2,000): %s", latencies(timeGets(t, db, keys, n-2000, n, 50_000, 3)))
	t.Logf("warm recent Get (newest 5%%): %s", latencies(timeGets(t, db, keys, recentLo, n, 50_000, 4)))
	t.Logf("warm old Get (oldest half): %s", latencies(timeGets(t, db, keys, 0, oldHi, 50_000, 5)))

	// Scan 100,000 Events of Project 1 from the middle of its range, as
	// Event processing does with EventsAfter.
	mid := keys[(n/2)*keyLen : (n/2+1)*keyLen]
	lower := slices.Clone(mid)
	binary.BigEndian.PutUint32(lower, 1)
	iter, err := db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: binary.BigEndian.AppendUint32(nil, 2)})
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	var count, size int
	for valid := iter.First(); valid && count < 100_000; valid = iter.Next() {
		v := bytes.Clone(iter.Value())
		size += len(v)
		count++
	}
	elapsed := time.Since(began)
	if err := iter.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("warm scan: %d Events, %s in %s, %.0f MB/s", count, mb(uint64(size)), elapsed.Round(time.Millisecond), float64(size)/(1<<20)/elapsed.Seconds())
}

// TestCompact compacts TestWrite's store down to the last level, where years
// of writes would eventually take most Events, and reports what that wrote
// and the space after. Then it times cold and warm Gets again. Run it after
// TestGet: it changes the store.
func TestCompact(t *testing.T) {
	e := envOrSkip(t)
	keys, user := e.written(t)
	n := len(keys) / keyLen
	ev := &events{}
	db := e.open(t, ev)
	began := time.Now()
	if err := db.Compact(context.Background(), []byte{0}, bytes.Repeat([]byte{0xff}, keyLen+1), false); err != nil {
		t.Fatal(err)
	}
	took := time.Since(began)
	settle(t, db, 20*time.Second)
	m := db.Metrics()
	a := measure(m)
	t.Logf("store %s: full compaction in %s wrote %s (%.2fx of user data)", e.name(), took.Round(time.Millisecond), mb(a.compacted), float64(a.compacted)/user)
	t.Logf("space: sst %s + blob %s = %s (%.2f bytes per user byte)", mb(a.tables), mb(a.blobs), mb(a.tables+a.blobs), float64(a.tables+a.blobs)/user)
	logLSM(t, m)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	dropPageCache(t, e.storeDir())
	db = e.open(t, ev)
	t.Logf("cold Get after full compaction: %s", latencies(timeGets(t, db, keys, 0, n, 1000, 6)))
	db.Close()
	warmPageCache(t, e.storeDir())
	db = e.open(t, ev)
	defer db.Close()
	t.Logf("warm Get after full compaction: %s", latencies(timeGets(t, db, keys, 0, n, 50_000, 7)))
}

// dirSizes sums the sizes of dir's files by extension.
func dirSizes(t *testing.T, dir string) string {
	t.Helper()
	sizes := map[string]int64{}
	counts := map[string]int{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		ext := filepath.Ext(path)
		if ext != ".sst" && ext != ".blob" {
			ext = "other"
		}
		sizes[ext] += info.Size()
		counts[ext]++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, ext := range []string{".sst", ".blob", "other"} {
		parts = append(parts, fmt.Sprintf("%d %s (%s)", counts[ext], ext, mb(uint64(sizes[ext]))))
	}
	return strings.Join(parts, ", ")
}

// TestTiering opens a store with a shared tier on a local directory, as
// config's "local" tier kind does, writes 256 MB of Events, then compacts
// everything, and reports where tables and blob files are. Then it reopens
// the store and reads every Event back.
func TestTiering(t *testing.T) {
	e := envOrSkip(t)
	payloads := newPayloads(payloadCount, 1)
	for _, c := range []struct {
		name     string
		strategy remote.CreateOnSharedStrategy
	}{
		// A tier with Level > 0 (storage/pebble.go): L5 and L6 on the tier.
		{"lower", remote.CreateOnSharedLower},
		// A tier with Level 0: every level on the tier.
		{"all", remote.CreateOnSharedAll},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(e.root, "tier-"+e.policy+"-"+c.name)
			local, shared := filepath.Join(dir, "local"), filepath.Join(dir, "shared")
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(shared, 0o755); err != nil {
				t.Fatal(err)
			}
			tier := func(o *pebble.Options) {
				o.Experimental.RemoteStorage = remote.MakeSimpleFactory(map[remote.Locator]remote.Storage{
					"cold": remote.NewLocalFS(shared, vfs.Default),
				})
				o.Experimental.CreateOnShared = c.strategy
				o.Experimental.CreateOnSharedLocator = "cold"
			}
			ev := &events{}
			db := openStore(t, local, e.policy, ev, tier)
			if err := db.SetCreatorID(1); err != nil {
				t.Fatal(err)
			}
			res := writeEvents(db, payloads, 256<<20)
			settle(t, db, 30*time.Second)
			m := db.Metrics()
			a := measure(m)
			t.Logf("after writes: write amp %.2fx (no WAL); local: %s; shared: %s",
				float64(a.flushed+a.compacted)/float64(res.userBytes), dirSizes(t, local), dirSizes(t, shared))
			logLSM(t, m)

			if err := db.Compact(context.Background(), []byte{0}, bytes.Repeat([]byte{0xff}, keyLen+1), false); err != nil {
				t.Fatal(err)
			}
			settle(t, db, 30*time.Second)
			m = db.Metrics()
			t.Logf("after a full compaction: local: %s; shared: %s", dirSizes(t, local), dirSizes(t, shared))
			logLSM(t, m)
			ev.mu.Lock()
			var localBlobs, sharedBlobs int
			for _, p := range ev.blobPaths {
				if strings.Contains(p, local) {
					localBlobs++
				} else {
					sharedBlobs++
				}
			}
			t.Logf("blob files created: %d, %d under the local dir, %d elsewhere; e.g. %v",
				len(ev.blobPaths), localBlobs, sharedBlobs, ev.blobPaths[:min(2, len(ev.blobPaths))])
			ev.mu.Unlock()
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			db = openStore(t, local, e.policy, ev, tier)
			defer db.Close()
			n := len(res.sums)
			for i := range n {
				key := res.keys[i*keyLen : (i+1)*keyLen]
				v, closer, err := db.Get(key)
				if err != nil {
					t.Fatalf("after reopen, get %x: %v", key, err)
				}
				if crc32.ChecksumIEEE(v) != res.sums[i] {
					t.Fatalf("after reopen, %x has a different value", key)
				}
				closer.Close()
			}
			t.Logf("after reopen: all %d Events read back intact", n)
			ev.mu.Lock()
			t.Logf("Pebble logged %d messages: %q", len(ev.messages), ev.messages)
			ev.mu.Unlock()
		})
	}
}

// TestClean deletes every store and key file.
func TestClean(t *testing.T) {
	e := envOrSkip(t)
	if err := os.RemoveAll(e.root); err != nil {
		t.Fatal(err)
	}
}
