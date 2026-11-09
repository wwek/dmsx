package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"dmsx/internal/storage"
)

// S3Uploader is the Qiniu/S3 path. manager.Uploader reads the parquet pipe and
// uploads one part at a time. Concurrency is 1 so the part buffer stays bounded.
type S3Uploader struct {
	Client   *s3.Client
	Bucket   string
	uploader *manager.Uploader
}

func NewS3Uploader(settings storage.S3Settings) (*S3Uploader, error) {
	endpoint := settings.Endpoint
	if endpoint != "" && !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	region := settings.Region
	if region == "" {
		region = "us-east-1"
	}
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(settings.AccessKey, settings.SecretKey, ""),
	}
	client := s3.NewFromConfig(cfg, func(opts *s3.Options) {
		if endpoint != "" {
			opts.BaseEndpoint = aws.String(endpoint)
		}
		opts.UsePathStyle = !settings.VirtualHost
	})
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 8 << 20
		u.Concurrency = 1
	})
	return &S3Uploader{Client: client, Bucket: settings.Bucket, uploader: uploader}, nil
}

func (u *S3Uploader) Upload(ctx context.Context, key string, body io.Reader) error {
	_, err := u.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(u.Bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	return err
}

func (u *S3Uploader) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := u.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(u.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (u *S3Uploader) Exists(ctx context.Context, key string) (bool, error) {
	_, err := u.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(u.Bucket), Key: aws.String(key)})
	if err == nil {
		return true, nil
	}
	var noSuch *s3types.NotFound
	if errors.As(err, &noSuch) {
		return false, nil
	}
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound {
		return false, nil
	}
	if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
		return false, nil
	}
	return false, fmt.Errorf("head %s: %w", key, err)
}
