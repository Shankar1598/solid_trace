package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cockroachdb/pebble/v2/objstorage/remote"
)

type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3Storage(ctx context.Context, bucket, prefix, endpoint string) (remote.Storage, error) {
	sdkOpts := []func(*config.LoadOptions) error{}
	if endpoint != "" {
		// Modern way to override endpoint in v2
		sdkOpts = append(sdkOpts, config.WithRegion("us-east-1")) // Region is still required
	}

	cfg, err := config.LoadDefaultConfig(ctx, sdkOpts...)
	if err != nil {
		return nil, err
	}
	return &S3Storage{
		client: s3.NewFromConfig(cfg, func(o *s3.Options) {
			if endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
			o.UsePathStyle = true // Often needed for S3 emulators
		}),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (s *S3Storage) Close() error { return nil }

func (s *S3Storage) fullPath(objName string) string {
	if s.prefix != "" {
		return path.Join(s.prefix, objName)
	}
	return objName
}

func (s *S3Storage) ReadObject(ctx context.Context, objName string) (remote.ObjectReader, int64, error) {
	key := s.fullPath(objName)
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, 0, err
	}
	size := aws.ToInt64(head.ContentLength)
	return &s3ObjectReader{
		client: s.client,
		bucket: s.bucket,
		key:    key,
	}, size, nil
}

func (s *S3Storage) CreateObject(objName string) (io.WriteCloser, error) {
	return &s3ObjectWriter{
		client: s.client,
		bucket: s.bucket,
		key:    s.fullPath(objName),
		buf:    new(bytes.Buffer),
	}, nil
}

func (s *S3Storage) List(prefix, delimiter string) ([]string, error) {
	var objects []string
	p := s.fullPath(prefix)
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Prefix:    aws.String(p),
		Delimiter: aws.String(delimiter),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			// Trim prefix
			name := *obj.Key
			if strings.HasPrefix(name, s.prefix+"/") {
				name = name[len(s.prefix)+1:]
			}
			objects = append(objects, name)
		}
	}
	return objects, nil
}

func (s *S3Storage) Delete(objName string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullPath(objName)),
	})
	return err
}

func (s *S3Storage) Size(objName string) (int64, error) {
	head, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullPath(objName)),
	})
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(head.ContentLength), nil
}

func (s *S3Storage) IsNotExistError(err error) bool {
	var nfe *types.NotFound
	var nsk *types.NoSuchKey
	return errors.As(err, &nfe) || errors.As(err, &nsk)
}

type s3ObjectReader struct {
	client *s3.Client
	bucket string
	key    string
}

func (r *s3ObjectReader) ReadAt(ctx context.Context, p []byte, offset int64) error {
	if len(p) == 0 {
		return nil
	}
	end := offset + int64(len(p)) - 1
	rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, end)

	resp, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(r.key),
		Range:  aws.String(rangeHeader),
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.ReadFull(resp.Body, p)
	return err
}

func (r *s3ObjectReader) Close() error { return nil }

type s3ObjectWriter struct {
	client *s3.Client
	bucket string
	key    string
	buf    *bytes.Buffer
}

func (w *s3ObjectWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *s3ObjectWriter) Close() error {
	_, err := w.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(w.key),
		Body:   bytes.NewReader(w.buf.Bytes()),
	})
	return err
}
