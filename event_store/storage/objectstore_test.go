package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
)

// minIOStore is an ObjectStore on the bucket SOLIDTRACE_TEST_S3_ENDPOINT
// serves, under a prefix of its own that is emptied after the test. The test
// skips without it; ~/.cache/solid-trace/logs-v2-notes/setup.md says how to
// run MinIO for it.
func minIOStore(t *testing.T) *s3Store {
	t.Helper()
	endpoint := os.Getenv("SOLIDTRACE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SOLIDTRACE_TEST_S3_ENDPOINT, and the bucket's key in SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET, to run against an S3 API")
	}
	bucket := os.Getenv("SOLIDTRACE_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "solidtrace-test"
	}
	cfg := &config.ObjectStore{
		Kind: config.ObjectStoreS3, Bucket: bucket, Prefix: "test-" + uuid.NewString() + "/",
		Region: "us-east-1", Endpoint: endpoint,
		KeyID: os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"), Secret: os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
	}
	store := NewObjectStore(cfg).(*s3Store)
	t.Cleanup(func() {
		ctx := context.Background()
		objects, err := store.List(ctx, "")
		if err != nil {
			t.Errorf("list the test's objects to delete them: %v", err)
		}
		for _, object := range objects {
			if err := store.Delete(ctx, object.Key); err != nil {
				t.Errorf("delete the test's object %s: %v", object.Key, err)
			}
		}
	})
	return store
}

// writeFile writes a local file and returns its path.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// testObjectStore checks what every ObjectStore does. read returns the bytes
// of key's object.
func testObjectStore(t *testing.T, store ObjectStore, read func(key string) string) {
	ctx := context.Background()
	hour7 := "project=1/day=2026-10-01/hour=07/data.parquet"
	hour8 := "project=1/day=2026-10-01/hour=08/data.parquet"
	other := "project=2/day=2026-10-01/hour=07/data.parquet"
	for key, content := range map[string]string{hour7: "hour 7", hour8: "hour eight", other: "project 2"} {
		if err := store.Put(ctx, key, writeFile(t, content)); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	if got := read(hour8); got != "hour eight" {
		t.Errorf("Expected hour 8's object to read back as put, got %q", got)
	}

	list := func(prefix string) []StoredObject {
		t.Helper()
		objects, err := store.List(ctx, prefix)
		if err != nil {
			t.Fatalf("List %q: %v", prefix, err)
		}
		return objects
	}
	want := []StoredObject{{hour7, 6}, {hour8, 10}}
	if got := list("project=1/"); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected project 1's objects %v, got %v", want, got)
	}
	if got := list(""); len(got) != 3 {
		t.Errorf("Expected 3 objects in all, got %v", got)
	}
	if got := list("project=3/"); len(got) != 0 {
		t.Errorf("Expected no objects of project 3, got %v", got)
	}

	// A Put replaces the object at its key.
	if err := store.Put(ctx, hour7, writeFile(t, "hour seven, again")); err != nil {
		t.Fatalf("Put %s again: %v", hour7, err)
	}
	if got := read(hour7); got != "hour seven, again" {
		t.Errorf("Expected hour 7's object replaced, got %q", got)
	}

	// Deleting twice is fine: a deleter may retry.
	for range 2 {
		if err := store.Delete(ctx, hour7); err != nil {
			t.Errorf("Delete %s: %v", hour7, err)
		}
	}
	if got, want := list(""), []StoredObject{{hour8, 10}, {other, 9}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v after the delete, got %v", want, got)
	}

	for _, key := range []string{"", "/project=1/data.parquet", "project=1/../../etc/passwd", "project=1//data.parquet"} {
		if err := store.Put(ctx, key, writeFile(t, "x")); err == nil {
			t.Errorf("Expected Put of the key %q to fail", key)
		}
	}
	if err := store.Put(ctx, "project=1/missing.parquet", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("Expected Put of a missing file to fail")
	}
}

func TestAFolderObjectStorePutsListsAndDeletesFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewDirStore(dir)
	testObjectStore(t, store, func(key string) string {
		data, err := os.ReadFile(store.URL(key))
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		return string(data)
	})
	if got, want := store.URL("project=1/data.parquet"), filepath.Join(dir, "project=1", "data.parquet"); got != want {
		t.Errorf("Expected the URL %s, got %s", want, got)
	}
	empty := NewDirStore(filepath.Join(dir, "never-made"))
	if objects, err := empty.List(context.Background(), ""); err != nil || len(objects) != 0 {
		t.Errorf("Expected a folder not made yet to list nothing, got %v (%v)", objects, err)
	}
}

func TestAnObjectStoreOnMinIOPutsListsAndDeletesObjects(t *testing.T) {
	store := minIOStore(t)
	testObjectStore(t, store, func(key string) string {
		out, err := store.client.GetObject(context.Background(), &s3.GetObjectInput{
			Bucket: aws.String(store.bucket), Key: aws.String(store.prefix + key),
		})
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		defer out.Body.Close()
		data, err := io.ReadAll(out.Body)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		return string(data)
	})
}

func TestAnObjectStoreOnMinIOListsPastOnePage(t *testing.T) {
	store := minIOStore(t)
	store.pageSize = 2
	ctx := context.Background()
	var want []StoredObject
	for _, key := range []string{"a/1", "a/2", "a/3", "a/4", "a/5", "b/1"} {
		if err := store.Put(ctx, key, writeFile(t, key)); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
		if key[0] == 'a' {
			want = append(want, StoredObject{key, 3})
		}
	}
	got, err := store.List(ctx, "a/")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v (%v)", want, got, err)
	}
}

func TestAnObjectStoresURLIsWhereDuckDBReadsIt(t *testing.T) {
	cfg := &config.ObjectStore{Kind: config.ObjectStoreGCS, Bucket: "acme", Prefix: "solidtrace/", Region: "auto", Endpoint: config.GCSEndpoint}
	if got, want := NewObjectStore(cfg).URL("project=1/day=2026-10-01/hour=07/data.parquet"), "s3://acme/solidtrace/project=1/day=2026-10-01/hour=07/data.parquet"; got != want {
		t.Errorf("Expected %s, got %s", want, got)
	}
	cfg.Prefix = ""
	if got, want := NewObjectStore(cfg).URL("events/1.parquet"), "s3://acme/events/1.parquet"; got != want {
		t.Errorf("Expected %s, got %s", want, got)
	}
}

func TestNoObjectStoreConfigIsNoStore(t *testing.T) {
	if store := NewObjectStore(nil); store != nil {
		t.Errorf("Expected no store, got %v", store)
	}
}

// GCS's S3 API answers 404 to a DELETE of a missing key, where S3 and MinIO
// answer 204.
func TestDeletingAMissingObjectOnGCSIsNotAnError(t *testing.T) {
	var deletes int
	gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		deletes++
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<?xml version='1.0' encoding='UTF-8'?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
	}))
	defer gcs.Close()
	store := NewObjectStore(&config.ObjectStore{
		Kind: config.ObjectStoreGCS, Bucket: "acme", Prefix: "solidtrace/", Region: "auto",
		Endpoint: gcs.URL, KeyID: "GOOG1EXAMPLE", Secret: "secret",
	})
	if err := store.Delete(context.Background(), "project=1/day=2026-10-01/hour=07/data.parquet"); err != nil || deletes != 1 {
		t.Errorf("Expected one DELETE and no error, got %d and %v", deletes, err)
	}
}
