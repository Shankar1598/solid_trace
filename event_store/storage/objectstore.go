package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/solidtrace/event_store/config"
)

// ObjectStore keeps files in a bucket, each under a key such as
// project=1/day=2026-10-01/hour=07/data.parquet. Keys are relative to the
// store's prefix. A nil ObjectStore means everything stays on local disk.
type ObjectStore interface {
	// Put uploads the file at localPath as key, replacing any object there.
	Put(ctx context.Context, key, localPath string) error
	// Delete deletes key's object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
	// List returns the objects whose keys begin with prefix, by key.
	List(ctx context.Context, prefix string) ([]StoredObject, error)
	// URL is where DuckDB reads key's object from.
	URL(key string) string
}

// StoredObject is one object of an ObjectStore.
type StoredObject struct {
	Key  string // relative to the store's prefix
	Size int64
}

// NewObjectStore returns the bucket of an object_store config, reached
// through the S3 API, or nil if cfg is nil.
func NewObjectStore(cfg *config.ObjectStore) ObjectStore {
	if cfg == nil {
		return nil
	}
	options := s3.Options{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.KeyID, cfg.Secret, ""),
		// GCS's S3 interop API rejects the checksums the SDK sends by
		// default, so send and check them only where S3 requires them.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if cfg.Endpoint != "" {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		// MinIO and GCS take the bucket in the path, not in the host name.
		options.UsePathStyle = true
	}
	return &s3Store{client: s3.New(options), bucket: cfg.Bucket, prefix: cfg.Prefix}
}

// s3Store is an ObjectStore on the S3 API: AWS S3, GCS with HMAC keys,
// MinIO or R2.
type s3Store struct {
	client *s3.Client
	bucket string
	prefix string // empty, or ending in "/"
	// pageSize caps the keys a List request asks for; 0 is the bucket's
	// own limit, 1,000 on S3. Tests set it to page through small lists.
	pageSize int32
}

// Put uploads the file in one PUT, its body read from the file as it is
// sent, so nothing is held in memory. One PUT takes up to 5 GB.
func (s *s3Store) Put(ctx context.Context, key, localPath string) error {
	if err := checkObjectKey(key); err != nil {
		return err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.prefix + key),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
	})
	if err != nil {
		return fmt.Errorf("upload %s to %s: %w", localPath, s.URL(key), err)
	}
	return nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	if err := checkObjectKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.prefix + key)})
	if err != nil && !isNotFound(err) {
		// S3 answers 204 for a missing key, GCS 404.
		return fmt.Errorf("delete %s: %w", s.URL(key), err)
	}
	return nil
}

// List pages through ListObjects (version 1), which GCS's S3 interop API
// has always taken.
func (s *s3Store) List(ctx context.Context, prefix string) ([]StoredObject, error) {
	objects := []StoredObject{}
	input := &s3.ListObjectsInput{Bucket: aws.String(s.bucket), Prefix: aws.String(s.prefix + prefix)}
	if s.pageSize > 0 {
		input.MaxKeys = aws.Int32(s.pageSize)
	}
	for {
		page, err := s.client.ListObjects(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s%s: %w", s.bucket, s.prefix, prefix, err)
		}
		for _, object := range page.Contents {
			objects = append(objects, StoredObject{
				Key:  strings.TrimPrefix(aws.ToString(object.Key), s.prefix),
				Size: aws.ToInt64(object.Size),
			})
		}
		if !aws.ToBool(page.IsTruncated) || len(page.Contents) == 0 {
			break
		}
		// Without a delimiter, the next page starts after the last key.
		input.Marker = page.Contents[len(page.Contents)-1].Key
	}
	slices.SortFunc(objects, func(a, b StoredObject) int { return strings.Compare(a.Key, b.Key) })
	return objects, nil
}

func (s *s3Store) URL(key string) string {
	return "s3://" + s.bucket + "/" + s.prefix + key
}

// isNotFound reports whether err is the bucket answering that an object
// isn't there.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
		return true
	}
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound
}

// checkObjectKey reports an error if key isn't a relative, slash-separated
// path inside the store: empty, absolute, or with a "." or ".." part.
func checkObjectKey(key string) error {
	if !fs.ValidPath(key) || key == "." {
		return fmt.Errorf("object key %q isn't a relative path such as project=1/day=2026-10-01/hour=07/data.parquet", key)
	}
	return nil
}

// NewDirStore returns an ObjectStore kept in a local folder, for tests. Its
// URLs are local paths, which DuckDB reads without httpfs.
func NewDirStore(dir string) ObjectStore {
	return &dirStore{dir: dir}
}

// dirStore is an ObjectStore in a local folder: each object is a file at its
// key below the folder.
type dirStore struct {
	dir string
}

func (d *dirStore) Put(_ context.Context, key, localPath string) error {
	if err := checkObjectKey(key); err != nil {
		return err
	}
	path := d.URL(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer src.Close()
	// Written aside and renamed, so a reader never sees half an object.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (d *dirStore) Delete(_ context.Context, key string) error {
	if err := checkObjectKey(key); err != nil {
		return err
	}
	if err := os.Remove(d.URL(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (d *dirStore) List(_ context.Context, prefix string) ([]StoredObject, error) {
	objects := []StoredObject{}
	err := filepath.WalkDir(d.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == d.dir {
				return fs.SkipAll
			}
			return err
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".put-") {
			return nil
		}
		rel, err := filepath.Rel(d.dir, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, StoredObject{Key: key, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(objects, func(a, b StoredObject) int { return strings.Compare(a.Key, b.Key) })
	return objects, nil
}

func (d *dirStore) URL(key string) string {
	return filepath.Join(d.dir, filepath.FromSlash(key))
}
