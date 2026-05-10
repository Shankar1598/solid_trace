package storage

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/cockroachdb/pebble/v2/objstorage/remote"
	"google.golang.org/api/iterator"
)

type GCSStorage struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCSStorage(ctx context.Context, bucket, prefix string) (remote.Storage, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCSStorage{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (s *GCSStorage) Close() error {
	return s.client.Close()
}

func (s *GCSStorage) fullPath(objName string) string {
	if s.prefix != "" {
		return path.Join(s.prefix, objName)
	}
	return objName
}

func (s *GCSStorage) ReadObject(ctx context.Context, objName string) (remote.ObjectReader, int64, error) {
	key := s.fullPath(objName)
	obj := s.client.Bucket(s.bucket).Object(key)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, 0, err
	}
	return &gcsObjectReader{
		obj: obj,
	}, attrs.Size, nil
}

func (s *GCSStorage) CreateObject(objName string) (io.WriteCloser, error) {
	obj := s.client.Bucket(s.bucket).Object(s.fullPath(objName))
	return &gcsObjectWriter{
		writer: obj.NewWriter(context.Background()),
	}, nil
}

func (s *GCSStorage) List(prefix, delimiter string) ([]string, error) {
	var objects []string
	p := s.fullPath(prefix)
	query := &storage.Query{Prefix: p}
	if delimiter != "" {
		query.Delimiter = delimiter
	}

	it := s.client.Bucket(s.bucket).Objects(context.Background(), query)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		name := attrs.Name
		if attrs.Prefix != "" { // handled by delimiter
			name = attrs.Prefix
		}

		if strings.HasPrefix(name, s.prefix+"/") {
			name = name[len(s.prefix)+1:]
		}
		objects = append(objects, name)
	}
	return objects, nil
}

func (s *GCSStorage) Delete(objName string) error {
	return s.client.Bucket(s.bucket).Object(s.fullPath(objName)).Delete(context.Background())
}

func (s *GCSStorage) Size(objName string) (int64, error) {
	attrs, err := s.client.Bucket(s.bucket).Object(s.fullPath(objName)).Attrs(context.Background())
	if err != nil {
		return 0, err
	}
	return attrs.Size, nil
}

func (s *GCSStorage) IsNotExistError(err error) bool {
	return errors.Is(err, storage.ErrObjectNotExist)
}

type gcsObjectReader struct {
	obj *storage.ObjectHandle
}

func (r *gcsObjectReader) ReadAt(ctx context.Context, p []byte, offset int64) error {
	if len(p) == 0 {
		return nil
	}
	rc, err := r.obj.NewRangeReader(ctx, offset, int64(len(p)))
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.ReadFull(rc, p)
	return err
}

func (r *gcsObjectReader) Close() error { return nil }

type gcsObjectWriter struct {
	writer *storage.Writer
}

func (w *gcsObjectWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *gcsObjectWriter) Close() error {
	return w.writer.Close()
}
