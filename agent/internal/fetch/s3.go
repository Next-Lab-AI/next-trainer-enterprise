package fetch

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func (f *Fetcher) fetchS3(ctx context.Context, u *url.URL, w io.Writer) error {
	cfg := f.s3Config()
	if cfg.Endpoint == "" {
		return fmt.Errorf("fetch: s3 endpoint not configured on node (S3_ENDPOINT)")
	}
	endpoint := cfg.Endpoint
	secure := false
	if rest, ok := strings.CutPrefix(endpoint, "https://"); ok {
		endpoint, secure = rest, true
	} else if rest, ok := strings.CutPrefix(endpoint, "http://"); ok {
		endpoint = rest
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return fmt.Errorf("fetch: s3 client: %w", err)
	}
	bucket := u.Host
	key := strings.TrimPrefix(u.Path, "/")
	obj, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("fetch: s3 get %s/%s: %w", bucket, key, err)
	}
	defer obj.Close()
	if _, err := io.Copy(w, obj); err != nil {
		return fmt.Errorf("fetch: s3 read %s/%s: %w", bucket, key, err)
	}
	return nil
}
