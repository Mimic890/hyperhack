package syncer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// fakeS3 serves ListObjectsV2 (two pages) and GetObject for bucket "blog".
func fakeS3(t *testing.T) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKEXAMPLE/") {
			http.Error(w, "unsigned", http.StatusForbidden)
			return
		}
		switch {
		case r.URL.Path == "/blog/" && r.URL.Query().Get("list-type") == "2":
			w.Header().Set("Content-Type", "application/xml")
			if r.URL.Query().Get("continuation-token") == "" {
				fmt.Fprint(w, `<ListBucketResult><Name>blog</Name><IsTruncated>true</IsTruncated><NextContinuationToken>t2</NextContinuationToken>
<Contents><Key>a.md</Key><ETag>"e1"</ETag><Size>3</Size></Contents>
<Contents><Key>b.md</Key><ETag>"e2"</ETag><Size>4</Size></Contents></ListBucketResult>`)
				return
			}
			fmt.Fprint(w, `<ListBucketResult><Name>blog</Name><IsTruncated>false</IsTruncated>
<Contents><Key>c.md</Key><ETag>"e3"</ETag><Size>5</Size></Contents></ListBucketResult>`)
		case r.URL.Path == "/blog/a.md":
			if r.Header.Get("If-Match") != `"e1"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code></Error>`)
				return
			}
			w.Header().Set("ETag", `"e1"`)
			w.Header().Set("Last-Modified", "Tue, 03 Nov 2026 12:00:00 GMT")
			fmt.Fprint(w, "abc")
		default:
			http.NotFound(w, r)
		}
	}))
}

func testBucket(t *testing.T, ts *httptest.Server) *s3Bucket {
	u, _ := url.Parse(ts.URL)
	c, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4("AKEXAMPLE", "secret", ""),
		Secure:       true,
		Region:       "garage",
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   1,
		Transport:    ts.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &s3Bucket{c: c, bucket: "blog"}
}

func TestS3Bucket(t *testing.T) {
	ts := fakeS3(t)
	defer ts.Close()
	b := testBucket(t, ts)
	ctx := context.Background()

	objs, truncated, err := b.List(ctx, 10)
	if err != nil || truncated || len(objs) != 3 {
		t.Fatalf("List = %v, %v, %v", objs, truncated, err)
	}
	if objs[0] != (Object{Key: "a.md", ETag: "e1", Size: 3}) {
		t.Errorf("objs[0] = %+v", objs[0])
	}
	if objs, truncated, err := b.List(ctx, 2); err != nil || !truncated || len(objs) != 2 {
		t.Fatalf("List(2) = %v, %v, %v", objs, truncated, err)
	}

	rc, err := b.Get(ctx, "a.md", "e1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || string(body) != "abc" {
		t.Fatalf("Get = %q, %v", body, err)
	}
	rc, err = b.Get(ctx, "a.md", "stale")
	if err == nil {
		_, err = io.ReadAll(rc)
		_ = rc.Close()
	}
	if err == nil {
		t.Fatal("Get with a stale ETag succeeded")
	}
}

func TestNewS3(t *testing.T) {
	u, _ := url.Parse("https://s3.example.com:3900")
	if _, err := NewS3(u, "garage", "blog", "ak", "sk"); err != nil {
		t.Fatal(err)
	}
}
