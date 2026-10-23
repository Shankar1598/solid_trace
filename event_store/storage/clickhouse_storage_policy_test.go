package storage_test

import (
	"encoding/xml"
	"testing"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/storage"
)

// storageConfig is what ClickHouse reads of ClickHouseStorageConfig.
type storageConfig struct {
	Disks struct {
		Bucket struct {
			Type               string `xml:"type"`
			Endpoint           string `xml:"endpoint"`
			Region             string `xml:"region"`
			KeyID              envRef `xml:"access_key_id"`
			Secret             envRef `xml:"secret_access_key"`
			SupportBatchDelete string `xml:"support_batch_delete"`
		} `xml:"bucket"`
		Cache struct {
			Type string `xml:"type"`
			Disk string `xml:"disk"`
		} `xml:"bucket_cache"`
	} `xml:"storage_configuration>disks"`
	Volumes []struct {
		XMLName  xml.Name
		Disk     string `xml:"disk"`
		Priority int    `xml:"volume_priority"`
	} `xml:"storage_configuration>policies>solidtrace>volumes>bucket"`
	MoveFactor string `xml:"storage_configuration>policies>solidtrace>move_factor"`
}

type envRef struct {
	FromEnv string `xml:"from_env,attr"`
}

func readStorageConfig(t *testing.T, bucket *config.ObjectStore) storageConfig {
	t.Helper()
	var got storageConfig
	if err := xml.Unmarshal(storage.ClickHouseStorageConfig(bucket), &got); err != nil {
		t.Fatalf("ClickHouse couldn't read the storage config: %v", err)
	}
	return got
}

func TestWithNoBucketTheStoragePolicyKeepsItsLocalVolumeOnly(t *testing.T) {
	got := readStorageConfig(t, nil)

	if got.Disks.Bucket.Type != "" || len(got.Volumes) != 0 {
		t.Errorf("storage config = %+v, want no disk or volume", got)
	}
}

func TestABucketAddsACachedVolumeOnItsClickHouseFolderAfterTheLocalOne(t *testing.T) {
	got := readStorageConfig(t, &config.ObjectStore{
		Kind: "s3", Bucket: "solidtrace-dev", Prefix: "dev/", Region: "us-east-1",
		Endpoint: "http://127.0.0.1:9000", KeyID: "never-written", Secret: "never-written",
	})

	bucket := got.Disks.Bucket
	if bucket.Type != "s3" || bucket.Endpoint != "http://127.0.0.1:9000/solidtrace-dev/dev/clickhouse/" || bucket.Region != "us-east-1" {
		t.Errorf("bucket disk = %+v, want s3 at http://127.0.0.1:9000/solidtrace-dev/dev/clickhouse/ in us-east-1", bucket)
	}
	// The key is read from EventStore's environment variables, never written.
	if bucket.KeyID.FromEnv != "SOLIDTRACE_OBJECT_STORE_KEY_ID" || bucket.Secret.FromEnv != "SOLIDTRACE_OBJECT_STORE_SECRET" {
		t.Errorf("bucket key = %+v / %+v, want the object store's environment variables", bucket.KeyID, bucket.Secret)
	}
	if bucket.SupportBatchDelete != "" {
		t.Errorf("batch delete = %q, want S3's default", bucket.SupportBatchDelete)
	}
	if c := got.Disks.Cache; c.Type != "cache" || c.Disk != "bucket" {
		t.Errorf("cache disk = %+v, want a cache of the bucket disk", c)
	}
	if len(got.Volumes) != 1 || got.Volumes[0].Disk != "bucket_cache" || got.Volumes[0].Priority != 2 {
		t.Errorf("volumes = %+v, want one on the cache, after the local volume", got.Volumes)
	}
	// Parts move to the bucket early once the local disk is 90% full.
	if got.MoveFactor != "0.1" {
		t.Errorf("move_factor = %q, want 0.1", got.MoveFactor)
	}
}

func TestAGCSBucketHasNoBatchDelete(t *testing.T) {
	got := readStorageConfig(t, &config.ObjectStore{
		Kind: "gcs", Bucket: "acme-logs", Prefix: "solidtrace/", Region: "auto",
		Endpoint: config.GCSEndpoint,
	})

	if b := got.Disks.Bucket; b.Endpoint != "https://storage.googleapis.com/acme-logs/solidtrace/clickhouse/" || b.SupportBatchDelete != "false" {
		t.Errorf("bucket disk = %+v, want GCS's endpoint with no batch delete", b)
	}
}

func TestAnAWSBucketIsReachedAtItsRegionsVirtualHost(t *testing.T) {
	got := readStorageConfig(t, &config.ObjectStore{Kind: "s3", Bucket: "logs", Region: "eu-west-1"})

	if want := "https://logs.s3.eu-west-1.amazonaws.com/clickhouse/"; got.Disks.Bucket.Endpoint != want {
		t.Errorf("endpoint = %q, want %q", got.Disks.Bucket.Endpoint, want)
	}
}
