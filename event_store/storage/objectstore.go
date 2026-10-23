package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
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
		APIOptions:                 []func(*middleware.Stack) error{signNothingGCSRewrites},
	}
	if cfg.Endpoint != "" {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		// MinIO and GCS take the bucket in the path, not in the host name.
		options.UsePathStyle = true
	}
	return &s3Store{client: s3.New(options), bucket: cfg.Bucket, prefix: cfg.Prefix}
}

// signNothingGCSRewrites keeps Accept-Encoding out of each request's
// signature and drops the x-id query parameter the SDK adds to PUT and
// DELETE. GCS's front end rewrites Accept-Encoding before GCS checks the
// signature, so a signed one never matches, and GCS rejects x-id
// (aws/aws-sdk-go-v2#1816; rclone does the same for GCS). S3, MinIO and R2
// need neither, so every kind gets it.
func signNothingGCSRewrites(stack *middleware.Stack) error {
	type acceptEncoding struct{}
	before := middleware.FinalizeMiddlewareFunc("UnsignAcceptEncodingAndDropXID",
		func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
			if req, ok := in.Request.(*smithyhttp.Request); ok {
				if value := req.Header.Get("Accept-Encoding"); value != "" {
					req.Header.Del("Accept-Encoding")
					// Kept in the context, as requests run concurrently.
					ctx = middleware.WithStackValue(ctx, acceptEncoding{}, value)
				}
				req.URL.RawQuery = withoutXID(req.URL.RawQuery)
			}
			return next.HandleFinalize(ctx, in)
		})
	after := middleware.FinalizeMiddlewareFunc("RestoreAcceptEncoding",
		func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
			// Sent unsigned, so Go's transport doesn't ask for gzip itself.
			if req, ok := in.Request.(*smithyhttp.Request); ok {
				if value, ok := middleware.GetStackValue(ctx, acceptEncoding{}).(string); ok {
					req.Header.Set("Accept-Encoding", value)
				}
			}
			return next.HandleFinalize(ctx, in)
		})
	return errors.Join(
		stack.Finalize.Insert(before, "Signing", middleware.Before),
		stack.Finalize.Insert(after, "Signing", middleware.After),
	)
}

// withoutXID returns a raw query without its x-id parameters, leaving the
// others exactly as they were encoded.
func withoutXID(rawQuery string) string {
	if !strings.Contains(rawQuery, "x-id=") {
		return rawQuery
	}
	parts := strings.Split(rawQuery, "&")
	parts = slices.DeleteFunc(parts, func(part string) bool { return strings.HasPrefix(part, "x-id=") })
	return strings.Join(parts, "&")
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
		return fmt.Errorf("upload %s to %s: %w", localPath, s.location(key), err)
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
		return fmt.Errorf("delete %s: %w", s.location(key), err)
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

// location names key's object in errors, as s3://<bucket>/<prefix><key>.
func (s *s3Store) location(key string) string {
	return "s3://" + s.bucket + "/" + s.prefix + key
}

// isNotFound reports whether err is the bucket answering NoSuchKey, as GCS
// does to a DELETE of a missing object. Any other 404 is not: NoSuchBucket,
// or one with no code (which the SDK calls NotFound) from something that
// isn't the bucket. Nothing was deleted, so a deleter must not count it as
// done.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey"
}

// checkObjectKey reports an error if key isn't a relative, slash-separated
// path inside the store: empty, absolute, or with a "." or ".." part.
func checkObjectKey(key string) error {
	if !fs.ValidPath(key) || key == "." {
		return fmt.Errorf("object key %q isn't a relative path such as project=1/day=2026-10-01/hour=07/data.parquet", key)
	}
	return nil
}
