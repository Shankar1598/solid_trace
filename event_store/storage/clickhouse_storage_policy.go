package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/logger"
)

// ClickHouseStoragePolicy is the storage policy EventStore's tables use. Its
// local volume is in deploy/clickhouse's config; with a bucket, EventStore adds
// a second volume on it (ClickHouseStorageConfig).
const ClickHouseStoragePolicy = "solidtrace"

// ClickHouseBucketVolume is the storage policy's volume in the bucket.
const ClickHouseBucketVolume = "bucket"

// clickHouseStorageFile is the file EventStore writes in ClickHouse's conf.d.
const clickHouseStorageFile = "solidtrace-storage.xml"

// clickHouseBucketCacheSize is the most local disk the bucket's read cache takes.
const clickHouseBucketCacheSize = "10Gi"

// ClickHouseStorageConfig is ClickHouse's config for the storage policy's
// bucket volume: an S3 disk in bucket under <prefix>clickhouse/, with a read
// cache, reached with the key ClickHouse finds in the same environment
// variables as EventStore. With no bucket it is empty, and the policy keeps
// its local volume only.
func ClickHouseStorageConfig(bucket *config.ObjectStore) []byte {
	type fromEnv struct {
		Var string `xml:"from_env,attr"`
	}
	type disk struct {
		Type     string   `xml:"type"`
		Endpoint string   `xml:"endpoint,omitempty"`
		Region   string   `xml:"region,omitempty"`
		KeyID    *fromEnv `xml:"access_key_id"`
		Secret   *fromEnv `xml:"secret_access_key"`
		// GCS's S3 API has no DeleteObjects.
		SupportBatchDelete *bool  `xml:"support_batch_delete"`
		Disk               string `xml:"disk,omitempty"`
		Path               string `xml:"path,omitempty"`
		MaxSize            string `xml:"max_size,omitempty"`
	}
	type volume struct {
		Disk     string `xml:"disk"`
		Priority int    `xml:"volume_priority"`
	}
	type disks struct {
		Bucket      disk `xml:"bucket"`
		BucketCache disk `xml:"bucket_cache"`
	}
	type root struct {
		XMLName xml.Name `xml:"clickhouse"`
		Disks   *disks   `xml:"storage_configuration>disks"`
		Volume  *volume  `xml:"storage_configuration>policies>solidtrace>volumes>bucket"`
		// The share of the local disk left free below which ClickHouse
		// moves the largest parts to the bucket, whatever their age.
		MoveFactor string `xml:"storage_configuration>policies>solidtrace>move_factor,omitempty"`
	}

	out := root{}
	if bucket != nil {
		s3 := disk{
			Type:     "s3",
			Endpoint: clickHouseBucketEndpoint(bucket),
			Region:   bucket.Region,
			KeyID:    &fromEnv{"SOLIDTRACE_OBJECT_STORE_KEY_ID"},
			Secret:   &fromEnv{"SOLIDTRACE_OBJECT_STORE_SECRET"},
		}
		if bucket.Kind == config.ObjectStoreGCS {
			no := false
			s3.SupportBatchDelete = &no
		}
		out.Disks = &disks{
			Bucket: s3,
			// The path is under ClickHouse's caches directory.
			BucketCache: disk{Type: "cache", Disk: "bucket", Path: "bucket/", MaxSize: clickHouseBucketCacheSize},
		}
		// After the local volume, whose priority is 1.
		out.Volume = &volume{Disk: "bucket_cache", Priority: 2}
		// ClickHouse's default with two volumes, written to be seen.
		out.MoveFactor = "0.1"
	}
	body, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // fixed types
	}
	return append([]byte("<!-- Written by EventStore from event_store.yml's object_store. -->\n"), append(body, '\n')...)
}

// clickHouseBucketEndpoint is the URL of the clickhouse/ folder under the
// bucket's prefix: path-style on an endpoint, virtual-hosted on AWS.
func clickHouseBucketEndpoint(bucket *config.ObjectStore) string {
	key := bucket.Prefix + "clickhouse/"
	if bucket.Endpoint == "" {
		return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket.Bucket, bucket.Region, key)
	}
	return bucket.Endpoint + "/" + bucket.Bucket + "/" + key
}

// bucketTTL is a family's main table's TTL with a bucket: a part moves to
// the bucket volume once its newest row's time, in timeColumn, is
// localCacheHours old. It is written as ClickHouse writes it back in
// system.tables.
func bucketTTL(timeColumn string, localCacheHours int) string {
	return fmt.Sprintf("toDateTime(%s) + toIntervalHour(%d) TO VOLUME '%s'", timeColumn, localCacheHours, ClickHouseBucketVolume)
}

// localCache is how long the day families' main tables keep their parts on
// local disk before they move to the bucket: Console's local_cache_hours,
// which UseLocalCacheHours sets as it changes.
type localCache struct {
	// mu holds UseLocalCacheHours to one at a time.
	mu sync.Mutex
	// set is the hours this process last set every main table's TTL to, or
	// -1 before it has.
	set int
	// hours is what LocalCacheHours answers.
	hours atomic.Int64
}

// useBucket, at start, sets the TTL of each family's main table
// (dayFamilies) with no bucket: none, so parts stay on local disk. With a
// bucket it leaves each TTL as it is, for UseLocalCacheHours to set from
// Console's setting, and takes the hours the logs table's TTL has for
// LocalCacheHours until then.
func (ch *ClickHouse) useBucket(ctx context.Context, bucket *config.ObjectStore) error {
	ch.localCache.set = -1
	ch.localCache.hours.Store(DefaultLocalCacheHours)
	if bucket == nil {
		for _, f := range dayFamilies {
			if err := ch.setTTL(ctx, f.Tables[0], ""); err != nil {
				return err
			}
		}
		return nil
	}
	have, err := ch.tableTTL(ctx, LogDays.Tables[0])
	if err != nil {
		return err
	}
	if m := ttlHours.FindStringSubmatch(have); m != nil {
		hours, _ := strconv.Atoi(m[1])
		ch.localCache.hours.Store(int64(hours))
	}
	return nil
}

// ttlHours finds the hours in a TTL bucketTTL wrote.
var ttlHours = regexp.MustCompile(`toIntervalHour\((\d+)\)`)

// LocalCacheHours is how long Logs and Spans stay on local disk before their
// parts move to the bucket, as last set (UseLocalCacheHours), or 0 with no
// bucket. Before this process has set it, it's the hours the logs table's
// TTL has, else the default.
func (ch *ClickHouse) LocalCacheHours() int {
	if !ch.bucket {
		return 0
	}
	return int(ch.localCache.hours.Load())
}

// UseLocalCacheHours sets each family's main table's TTL so that a part
// moves to the bucket once its newest row is hours old, and 0 moves it once
// stored. It touches ClickHouse only when hours differ from those it last
// set, and alters a table only when its TTL differs, since ClickHouse then
// works out every part's move time again in the background, reading each
// part's time column, from the bucket too. With no bucket it does nothing.
//
// Parts move there earlier too: ClickHouse moves the largest once its local
// disk is 90% full (the policy's move_factor, 0.1 by default), and the disk
// limit moves the oldest days (EnforceDiskLimit).
func (ch *ClickHouse) UseLocalCacheHours(ctx context.Context, hours int) error {
	if !ch.bucket {
		return nil
	}
	ch.localCache.mu.Lock()
	defer ch.localCache.mu.Unlock()
	if ch.localCache.set == hours {
		return nil
	}
	for _, f := range dayFamilies {
		if err := ch.setTTL(ctx, f.Tables[0], bucketTTL(f.TimeColumn, hours)); err != nil {
			return err
		}
	}
	ch.localCache.set = hours
	ch.localCache.hours.Store(int64(hours))
	return nil
}

// tableTTL is a table's TTL as system.tables writes it, or "" for none.
func (ch *ClickHouse) tableTTL(ctx context.Context, table string) (string, error) {
	var engine string
	if err := ch.conn.QueryRow(ctx,
		"SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = ?", table).Scan(&engine); err != nil {
		return "", fmt.Errorf("reading the %s table's TTL: %w", table, err)
	}
	var have string
	if _, rest, ok := strings.Cut(engine, " TTL "); ok {
		have, _, _ = strings.Cut(rest, " SETTINGS ")
	}
	return have, nil
}

// setTTL sets a table's TTL to want, or removes it for "", when it differs.
func (ch *ClickHouse) setTTL(ctx context.Context, table, want string) error {
	have, err := ch.tableTTL(ctx, table)
	if err != nil {
		return err
	}
	if have == want {
		return nil
	}
	alter := fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s", table, want)
	if want == "" {
		alter = fmt.Sprintf("ALTER TABLE %s REMOVE TTL", table)
	}
	if err := ch.conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("setting when the %s table's parts move to the bucket: %w", table, err)
	}
	logger.L.Info("Set when a table's parts move to the bucket", "table", table, "was", have, "now", want)
	return nil
}

// useStoragePolicy writes the storage policy's config for bucket in dir,
// ClickHouse's conf.d, and with a bucket waits until ClickHouse has loaded
// its volume. ClickHouse reloads its config by itself.
func (ch *ClickHouse) useStoragePolicy(ctx context.Context, dir string, bucket *config.ObjectStore) error {
	path := filepath.Join(dir, clickHouseStorageFile)
	want := ClickHouseStorageConfig(bucket)
	if have, err := os.ReadFile(path); err != nil || !bytes.Equal(have, want) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp := filepath.Join(dir, "."+clickHouseStorageFile+".tmp")
		if err := os.WriteFile(tmp, want, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	if bucket == nil {
		return nil
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		var volumes uint64
		err := ch.conn.QueryRow(ctx,
			"SELECT count() FROM system.storage_policies WHERE policy_name = ? AND volume_name = ?",
			ClickHouseStoragePolicy, ClickHouseBucketVolume).Scan(&volumes)
		if err != nil {
			return fmt.Errorf("reading ClickHouse's storage policies: %w", err)
		}
		if volumes == 1 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ClickHouse's %s storage policy has no %s volume 30 s after %s was written: is config_dir ClickHouse's conf.d? ClickHouse's log says why it didn't load the bucket", ClickHouseStoragePolicy, ClickHouseBucketVolume, path)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
