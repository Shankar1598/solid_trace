package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		Type     string `xml:"type"`
		Endpoint string `xml:"endpoint,omitempty"`
		Region   string `xml:"region,omitempty"`
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

// logsBucketTTL is the logs table's TTL with a bucket: a part moves to the
// bucket volume once its newest Log is localCacheHours old. It is written as
// ClickHouse writes it back in system.tables.
func logsBucketTTL(localCacheHours int) string {
	return fmt.Sprintf("toDateTime(time) + toIntervalHour(%d) TO VOLUME '%s'", localCacheHours, ClickHouseBucketVolume)
}

// useBucket sets the logs table's TTL for bucket: with one, parts move to it
// after its local_cache_hours; with none, they stay on local disk. It alters
// the table only when its TTL differs, since ClickHouse then works out every
// part's TTL again.
//
// Parts move there earlier too: ClickHouse moves the largest once its local
// disk is 90% full (the policy's move_factor, 0.1 by default), and the disk
// limit moves the oldest days (EnforceDiskLimit).
func (ch *ClickHouse) useBucket(ctx context.Context, bucket *config.ObjectStore) error {
	var engine string
	if err := ch.conn.QueryRow(ctx,
		"SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = 'logs'").Scan(&engine); err != nil {
		return fmt.Errorf("reading the logs table's TTL: %w", err)
	}
	var have, want string
	if _, rest, ok := strings.Cut(engine, " TTL "); ok {
		have, _, _ = strings.Cut(rest, " SETTINGS ")
	}
	if bucket != nil {
		want = logsBucketTTL(bucket.LocalCacheHours)
	}
	if have == want {
		return nil
	}
	alter := "ALTER TABLE logs MODIFY TTL " + want
	if want == "" {
		alter = "ALTER TABLE logs REMOVE TTL"
	}
	if err := ch.conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("setting when the logs table's parts move to the bucket: %w", err)
	}
	logger.L.Info("Set when the logs table's parts move to the bucket", "was", have, "now", want)
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
