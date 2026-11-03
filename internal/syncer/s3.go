package syncer

import (
	"context"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Object is one bucket entry.
type Object struct {
	Key  string
	ETag string
	Size int64
}

// Bucket is the read-only S3 access the syncer needs.
type Bucket interface {
	// List returns up to max objects in key order; truncated is true if there are more.
	List(ctx context.Context, max int) (objs []Object, truncated bool, err error)
	// Get streams an object. It fails if the object no longer has this ETag.
	Get(ctx context.Context, key, etag string) (io.ReadCloser, error)
}

const listTimeout = 2 * time.Minute

type s3Bucket struct {
	c      *minio.Client
	bucket string
}

// NewS3 connects to an S3 API (Garage) with path-style addressing over HTTPS.
func NewS3(endpoint *url.URL, region, bucket, accessKey, secretKey string) (Bucket, error) {
	c, err := minio.New(endpoint.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure:       true,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   1, // the syncer retries and logs each attempt
	})
	if err != nil {
		return nil, err
	}
	return &s3Bucket{c: c, bucket: bucket}, nil
}

func (b *s3Bucket) List(ctx context.Context, max int) ([]Object, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel() // also stops the listing goroutine if we return early
	var out []Object
	for o := range b.c.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if o.Err != nil {
			return nil, false, o.Err
		}
		if len(out) == max {
			return out, true, nil
		}
		out = append(out, Object{Key: o.Key, ETag: o.ETag, Size: o.Size})
	}
	return out, false, nil
}

func (b *s3Bucket) Get(ctx context.Context, key, etag string) (io.ReadCloser, error) {
	var opts minio.GetObjectOptions
	if err := opts.SetMatchETag(etag); err != nil {
		return nil, err
	}
	return b.c.GetObject(ctx, b.bucket, key, opts)
}
