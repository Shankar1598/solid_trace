package clickhousetest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/solidtrace/event_store/config"
)

// BucketPolicy is the storage policy Bucket gives tests: EventStore's two
// volumes, local and bucket, with the bucket volume on the S3 stand-in.
const BucketPolicy = "solidtrace_test_bucket"

// bucketPolicyFile is where Bucket writes BucketPolicy, in the dev
// ClickHouse's conf.d.
const bucketPolicyFile = "solidtrace-test-bucket.xml"

// Bucket gives a test a bucket tier: it makes sure the dev ClickHouse has
// BucketPolicy, and returns the object_store EventStore would be configured
// with for the same bucket. A test makes EventStore's tables on BucketPolicy
// through config.ClickHouse.StoragePolicy. Without an S3 stand-in, it skips
// the test.
//
// The S3 stand-in is SOLIDTRACE_TEST_S3_ENDPOINT, with its key in
// SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET, or else
// the dev MinIO at http://127.0.0.1:9000 with its key in
// ~/.cache/solid-trace/minio/env. The bucket is SOLIDTRACE_TEST_S3_BUCKET, or
// solidtrace-test, and ClickHouse keeps its parts there under
// clickhouse-test/.
//
// The policy is written into the conf.d bin/clickhouse-dev mounts (the main
// checkout's storage/clickhouse/conf.d), and stays: ClickHouse can't drop a
// policy while it runs. Its key is written into it, as the dev ClickHouse
// may have started without the key in its environment. Its move_factor is
// 0, so that only the TTL and the disk limit move parts: the dev box's disk
// is often fuller than the default 0.1 allows, which would move every part
// at once.
func Bucket(t testing.TB) *config.ObjectStore {
	t.Helper()
	bucket := s3StandIn(t)
	dir, err := devConfDir()
	if err != nil {
		t.Fatalf("Finding the dev ClickHouse's conf.d: %v", err)
	}
	path := filepath.Join(dir, bucketPolicyFile)
	want := bucketPolicyConfig(bucket)
	if have, err := os.ReadFile(path); err != nil || !bytes.Equal(have, want) {
		if err := os.WriteFile(path, want, 0o600); err != nil {
			t.Fatalf("Writing the bucket's storage policy: %v", err)
		}
	}
	if err := waitForBucketPolicy(); err != nil {
		t.Fatalf("%v (is %s the dev ClickHouse's conf.d? its log says why it didn't load %s)", err, dir, bucketPolicyFile)
	}
	return bucket
}

// S3StandIn is the object_store of the S3 stand-in that Bucket uses, with
// no prefix, or skips the test without one. Unlike Bucket, it leaves the dev
// ClickHouse's storage policies alone.
func S3StandIn(t testing.TB) *config.ObjectStore {
	t.Helper()
	return s3StandIn(t)
}

// UserFiles is the directory the dev ClickHouse reads files from with
// file(), its user_files_path: SOLIDTRACE_TEST_CLICKHOUSE_USER_FILES, or the
// main checkout's storage/duckdb/development/logs_parquet, which
// bin/clickhouse-dev mounts there. A file at <UserFiles>/a/b is file('a/b')
// to it. It skips the test when the directory is missing.
func UserFiles(t testing.TB) string {
	t.Helper()
	dir := os.Getenv("SOLIDTRACE_TEST_CLICKHOUSE_USER_FILES")
	if dir == "" {
		devDir, err := devDir()
		if err != nil {
			t.Skipf("can't find the dev ClickHouse's user files: %v", err)
		}
		dir = filepath.Join(filepath.Dir(devDir), "duckdb", "development", "logs_parquet")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("the dev ClickHouse's user files aren't on this machine: %v", err)
	}
	return dir
}

// s3StandIn is the object_store of the S3 stand-in, or skips the test.
func s3StandIn(t testing.TB) *config.ObjectStore {
	t.Helper()
	bucket := &config.ObjectStore{
		Kind:            config.ObjectStoreS3,
		Bucket:          os.Getenv("SOLIDTRACE_TEST_S3_BUCKET"),
		Region:          "us-east-1",
		Endpoint:        os.Getenv("SOLIDTRACE_TEST_S3_ENDPOINT"),
		KeyID:           os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"),
		Secret:          os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
		LocalCacheHours: 24,
	}
	if bucket.Bucket == "" {
		bucket.Bucket = "solidtrace-test"
	}
	if bucket.Endpoint != "" {
		return bucket
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no S3 stand-in: set SOLIDTRACE_TEST_S3_ENDPOINT, or run the dev MinIO")
	}
	env, err := os.ReadFile(filepath.Join(home, ".cache", "solid-trace", "minio", "env"))
	if err != nil {
		t.Skip("no S3 stand-in: set SOLIDTRACE_TEST_S3_ENDPOINT, or run the dev MinIO with its key in ~/.cache/solid-trace/minio/env")
	}
	lines := bufio.NewScanner(bytes.NewReader(env))
	for lines.Scan() {
		name, value, _ := strings.Cut(lines.Text(), "=")
		switch name {
		case "MINIO_ROOT_USER":
			bucket.KeyID = value
		case "MINIO_ROOT_PASSWORD":
			bucket.Secret = value
		}
	}
	bucket.Endpoint = "http://127.0.0.1:9000"
	return bucket
}

// devConfDir is the conf.d bin/clickhouse-dev mounts into the dev
// ClickHouse: SOLIDTRACE_TEST_CLICKHOUSE_CONF_D, or the main checkout's
// storage/clickhouse/conf.d.
func devConfDir() (string, error) {
	if dir := os.Getenv("SOLIDTRACE_TEST_CLICKHOUSE_CONF_D"); dir != "" {
		return dir, nil
	}
	dir, err := devDir()
	return filepath.Join(dir, "conf.d"), err
}

// DataDir is the directory bin/clickhouse-dev keeps the dev ClickHouse's
// data in, its local disk: SOLIDTRACE_TEST_CLICKHOUSE_DATA, or the main
// checkout's storage/clickhouse/data. It skips the test when it's missing.
func DataDir(t testing.TB) string {
	t.Helper()
	dir := os.Getenv("SOLIDTRACE_TEST_CLICKHOUSE_DATA")
	if dir == "" {
		devDir, err := devDir()
		if err != nil {
			t.Skipf("can't find the dev ClickHouse's data: %v", err)
		}
		dir = filepath.Join(devDir, "data")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("the dev ClickHouse's data isn't on this machine: %v", err)
	}
	return dir
}

// devDir is bin/clickhouse-dev's storage/clickhouse in the main checkout.
func devDir() (string, error) {
	// This file is <checkout>/event_store/pkg/clickhousetest/bucket.go.
	_, self, _, _ := runtime.Caller(0)
	checkout := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(self))))
	out, err := exec.Command("git", "-C", checkout, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", err
	}
	main := filepath.Dir(strings.TrimSpace(string(out)))
	return filepath.Join(main, "storage", "clickhouse"), nil
}

// bucketPolicyConfig is BucketPolicy's config, as EventStore's own policy
// with a bucket (storage.ClickHouseStorageConfig), with the key written in.
func bucketPolicyConfig(bucket *config.ObjectStore) []byte {
	text := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	return fmt.Appendf(nil, `<!-- Written by EventStore's tests (pkg/clickhousetest): a storage policy with a bucket on the S3 stand-in. -->
<clickhouse>
  <storage_configuration>
    <disks>
      <%[1]s>
        <type>s3</type>
        <endpoint>%[2]s</endpoint>
        <region>%[3]s</region>
        <access_key_id>%[4]s</access_key_id>
        <secret_access_key>%[5]s</secret_access_key>
      </%[1]s>
      <%[1]s_cache>
        <type>cache</type>
        <disk>%[1]s</disk>
        <path>%[1]s/</path>
        <max_size>1Gi</max_size>
      </%[1]s_cache>
    </disks>
    <policies>
      <%[1]s>
        <volumes>
          <local>
            <disk>default</disk>
            <volume_priority>1</volume_priority>
          </local>
          <bucket>
            <disk>%[1]s_cache</disk>
            <volume_priority>2</volume_priority>
          </bucket>
        </volumes>
        <move_factor>0</move_factor>
      </%[1]s>
    </policies>
  </storage_configuration>
</clickhouse>
`, BucketPolicy, text(bucket.Endpoint+"/"+bucket.Bucket+"/clickhouse-test/"), text(bucket.Region), text(bucket.KeyID), text(bucket.Secret))
}

// waitForBucketPolicy waits up to 30 s for the dev ClickHouse to load
// BucketPolicy's bucket volume.
func waitForBucketPolicy() error {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:        []string{devAddress},
		Auth:        clickhouse.Auth{Username: devUser, Password: os.Getenv("SOLIDTRACE_CLICKHOUSE_PASSWORD")},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		var volumes uint64
		if err := conn.QueryRow(ctx, "SELECT count() FROM system.storage_policies WHERE policy_name = ? AND volume_name = 'bucket'",
			BucketPolicy).Scan(&volumes); err != nil {
			return fmt.Errorf("reading the dev ClickHouse's storage policies: %w", err)
		}
		if volumes == 1 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the dev ClickHouse has no %s storage policy 30 s after %s was written", BucketPolicy, bucketPolicyFile)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
