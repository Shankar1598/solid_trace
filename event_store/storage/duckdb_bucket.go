package storage

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// bucketSecret is the name of the DuckDB secret that reads the bucket.
const bucketSecret = "solidtrace_bucket"

// remoteReadThreads is how many DuckDB threads each CPU gets while a bucket
// is configured.
const remoteReadThreads = 4

// UseBucket lets every pooled connection read the bucket's s3:// URLs
// (ObjectStore.URL). It loads httpfs from extensionDir, where the Docker
// image puts it, by name: DuckDB never installs or loads an extension by
// itself (NewDuckDBWriter). It makes the bucket's secret, in memory only,
// and turns on the caches for remote Parquet: footers, HTTP metadata and
// file ranges. The caches stay within memory_limit.
//
// It also gives DuckDB remoteReadThreads threads for each CPU, not one:
// httpfs holds a thread for each request it waits on, so threads cap the
// requests in flight. The object-store probe's cold 7-day chart took 1.9 s
// at 16 threads on 4 CPUs, and 5.2 s at 4.
//
// Call it once, right after NewDuckDBWriter, and only when a bucket is
// configured: without one, nothing changes.
func (w *DuckDBWriter) UseBucket(store *config.ObjectStore, extensionDir string) error {
	// Settings are global, and LOAD and a temporary secret belong to the
	// database, so each serves every pooled connection.
	if _, err := w.db.Exec("SET GLOBAL extension_directory = " + sqltext.String(extensionDir)); err != nil {
		return fmt.Errorf("set DuckDB's extension directory: %w", err)
	}
	if _, err := w.db.Exec("LOAD httpfs"); err != nil {
		return fmt.Errorf("load DuckDB's httpfs extension, which reads the bucket, from %s: %w", w.extensionPath(extensionDir, "httpfs"), err)
	}
	if _, err := w.db.Exec(bucketSecretSQL(store)); err != nil {
		// DuckDB's error may quote the statement, and with it the key.
		message := err.Error()
		if store.Secret != "" {
			message = strings.ReplaceAll(message, store.Secret, "…")
			message = strings.ReplaceAll(message, strings.ReplaceAll(store.Secret, "'", "''"), "…")
		}
		return fmt.Errorf("create the bucket's DuckDB secret: %s", message)
	}
	for _, setting := range []string{
		"SET GLOBAL parquet_metadata_cache = true",
		"SET GLOBAL enable_http_metadata_cache = true",
		"SET GLOBAL enable_external_file_cache = true",
		fmt.Sprintf("SET GLOBAL threads = %d", remoteReadThreads*runtime.NumCPU()),
	} {
		if _, err := w.db.Exec(setting); err != nil {
			return fmt.Errorf("%s: %w", setting, err)
		}
	}
	return nil
}

// extensionPath is where DuckDB looks for an extension in dir, or dir if
// DuckDB can't say.
func (w *DuckDBWriter) extensionPath(dir, name string) string {
	var version, platform string
	if err := w.db.QueryRow("SELECT library_version, (SELECT platform FROM pragma_platform()) FROM pragma_version()").Scan(&version, &platform); err != nil {
		return dir
	}
	return fmt.Sprintf("%s/%s/%s/%s.duckdb_extension (download https://extensions.duckdb.org/%s/%s/%s.duckdb_extension.gz and gunzip it there)",
		strings.TrimSuffix(dir, "/"), version, platform, name, version, platform, name)
}

// bucketSecretSQL is the CREATE SECRET for the bucket: an S3 secret, scoped
// to the bucket, that reaches GCS, MinIO or R2 through their endpoint.
func bucketSecretSQL(store *config.ObjectStore) string {
	options := []string{
		"TYPE s3",
		"KEY_ID " + sqltext.String(store.KeyID),
		"SECRET " + sqltext.String(store.Secret),
		"REGION " + sqltext.String(store.Region),
		"SCOPE " + sqltext.String("s3://"+store.Bucket+"/"),
	}
	if store.Endpoint != "" {
		// The config checked the endpoint is an http or https URL.
		endpoint, _ := url.Parse(store.Endpoint)
		options = append(options,
			"ENDPOINT "+sqltext.String(endpoint.Host),
			"URL_STYLE 'path'",
			fmt.Sprintf("USE_SSL %t", endpoint.Scheme == "https"))
	}
	return fmt.Sprintf("CREATE OR REPLACE SECRET %s (%s)", bucketSecret, strings.Join(options, ", "))
}
