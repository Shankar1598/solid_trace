package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
)

// minIOConfig is an object_store config for the bucket
// SOLIDTRACE_TEST_S3_ENDPOINT serves, under a prefix of its own. The test
// skips without it; ~/.cache/solid-trace/logs-v2-notes/setup.md says how to
// run MinIO for it.
func minIOConfig(t *testing.T) *config.ObjectStore {
	t.Helper()
	endpoint := os.Getenv("SOLIDTRACE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SOLIDTRACE_TEST_S3_ENDPOINT, and the bucket's key in SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET, to run against an S3 API")
	}
	bucket := os.Getenv("SOLIDTRACE_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "solidtrace-test"
	}
	return &config.ObjectStore{
		Kind: config.ObjectStoreS3, Bucket: bucket, Prefix: "test-" + uuid.NewString() + "/",
		Region: "us-east-1", Endpoint: endpoint,
		KeyID: os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"), Secret: os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
	}
}

// minIOStore is an ObjectStore on cfg's bucket, whose objects are deleted
// after the test.
func minIOStore(t *testing.T, cfg *config.ObjectStore) *s3Store {
	t.Helper()
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

func TestAnObjectStoreOnMinIOPutsListsAndDeletesObjects(t *testing.T) {
	store := minIOStore(t, minIOConfig(t))
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
	store := minIOStore(t, minIOConfig(t))
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

func TestDeletingFromAMissingBucketOnMinIOIsAnError(t *testing.T) {
	cfg := minIOConfig(t)
	cfg.Bucket = "solidtrace-no-such-bucket"
	if err := NewObjectStore(cfg).Delete(context.Background(), "project=1/day=2026-10-01/hour=07/data.parquet"); err == nil {
		t.Error("Expected a delete from a missing bucket to fail, as it deleted nothing")
	}
}

func TestNoObjectStoreConfigIsNoStore(t *testing.T) {
	if store := NewObjectStore(nil); store != nil {
		t.Errorf("Expected no store, got %v", store)
	}
}

// GCS's S3 API answers 404 to a DELETE of a missing key, where S3 and MinIO
// answer 204. A 404 for anything else, such as a missing bucket, deleted
// nothing, so it is an error: a deleter mustn't count it as done.
func TestDeletingAMissingObjectOnGCSIsNotAnError(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantErr    bool
	}{
		{"a missing key", `<?xml version='1.0' encoding='UTF-8'?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`, false},
		{"a missing bucket", `<?xml version='1.0' encoding='UTF-8'?><Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist.</Message></Error>`, true},
		{"a 404 without a code", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var deletes atomic.Int32
			gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					w.WriteHeader(http.StatusNotImplemented)
					return
				}
				deletes.Add(1)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, test.body)
			}))
			defer gcs.Close()
			store := NewObjectStore(&config.ObjectStore{
				Kind: config.ObjectStoreGCS, Bucket: "acme", Prefix: "solidtrace/", Region: "auto",
				Endpoint: gcs.URL, KeyID: "GOOG1EXAMPLE", Secret: "secret",
			})
			err := store.Delete(context.Background(), "project=1/day=2026-10-01/hour=07/data.parquet")
			if (err != nil) != test.wantErr || deletes.Load() != 1 {
				t.Errorf("Expected one DELETE and an error %v, got %d and %v", test.wantErr, deletes.Load(), err)
			}
		})
	}
}

// GCS's front end rewrites Accept-Encoding (to "identity,gzip(gfe)") before
// it checks a request's signature, and it rejects the SDK's x-id query
// parameter. So no request may sign Accept-Encoding or carry x-id.
func TestAnObjectStoresRequestsSignNoHeaderGCSRewrites(t *testing.T) {
	type request struct{ method, query, signedHeaders, acceptEncoding string }
	var (
		mu       sync.Mutex
		requests []request
	)
	gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		signed := r.Header.Get("Authorization")
		if _, after, ok := strings.Cut(signed, "SignedHeaders="); ok {
			signed, _, _ = strings.Cut(after, ",")
		}
		requests = append(requests, request{r.Method, r.URL.RawQuery, signed, r.Header.Get("Accept-Encoding")})
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			io.WriteString(w, `<?xml version='1.0' encoding='UTF-8'?><ListBucketResult xmlns="http://doc.s3.amazonaws.com/2006-03-01"><Name>acme</Name><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer gcs.Close()
	store := NewObjectStore(&config.ObjectStore{
		Kind: config.ObjectStoreGCS, Bucket: "acme", Prefix: "solidtrace/", Region: "auto",
		Endpoint: gcs.URL, KeyID: "GOOG1EXAMPLE", Secret: "secret",
	})
	ctx := context.Background()
	key := "project=1/day=2026-10-01/hour=07/data.parquet"
	if err := store.Put(ctx, key, writeFile(t, "hour 7")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.List(ctx, "project=1/"); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("Expected a PUT, a GET and a DELETE, got %v", requests)
	}
	for _, r := range requests {
		if !strings.Contains(r.signedHeaders, "host") {
			t.Errorf("Expected %s to be signed, got the signed headers %q", r.method, r.signedHeaders)
		}
		if slices.Contains(strings.Split(r.signedHeaders, ";"), "accept-encoding") {
			t.Errorf("Expected %s not to sign Accept-Encoding, got the signed headers %q", r.method, r.signedHeaders)
		}
		if strings.Contains(r.query, "x-id") {
			t.Errorf("Expected %s to have no x-id, got the query %q", r.method, r.query)
		}
		// Sent unsigned, so Go's transport doesn't ask for gzip itself.
		if r.acceptEncoding != "identity" {
			t.Errorf("Expected %s to send Accept-Encoding identity, got %q", r.method, r.acceptEncoding)
		}
	}
	if got := requests[1].query; !strings.Contains(got, "prefix=solidtrace%2Fproject%3D1%2F") {
		t.Errorf("Expected the GET to keep its prefix, got the query %q", got)
	}
}
