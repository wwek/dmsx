package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Settings is shared by AWS S3, MinIO, Cloudflare R2 and Qiniu's S3 endpoint.
type S3Settings struct {
	Bucket       string `json:"bucket"`
	Region       string `json:"region"`
	Endpoint     string `json:"endpoint"`
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	Prefix       string `json:"prefix"`
	VirtualHost  bool   `json:"virtualHost"`
	SkipTLS      bool   `json:"skipTLS"`
	StorageClass string `json:"storageClass"`
}

type s3Store struct {
	settings S3Settings
	qiniu    bool
	core     *minio.Core
	layout   Layout
}

var qiniuEndpoints = map[string]string{
	"cn-east-1":      "s3-cn-east-1.qiniucs.com",
	"cn-north-1":     "s3-cn-north-1.qiniucs.com",
	"cn-south-1":     "s3-cn-south-1.qiniucs.com",
	"us-north-1":     "s3-us-north-1.qiniucs.com",
	"ap-southeast-1": "s3-ap-southeast-1.qiniucs.com",
}

func defaultGCS(raw json.RawMessage) json.RawMessage {
	var settings S3Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return raw
	}
	if settings.Endpoint == "" {
		settings.Endpoint = "https://storage.googleapis.com"
		settings.VirtualHost = true
	}
	out, err := json.Marshal(settings)
	if err != nil {
		return raw
	}
	return out
}

func defaultOSS(raw json.RawMessage) json.RawMessage {
	var settings S3Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return raw
	}
	if settings.Endpoint == "" && settings.Region != "" {
		settings.Endpoint = "https://oss-" + settings.Region + ".aliyuncs.com"
		settings.VirtualHost = true
	}
	out, err := json.Marshal(settings)
	if err != nil {
		return raw
	}
	return out
}

func newOSS(raw json.RawMessage) (Store, error) {
	prepared := defaultOSS(raw)
	var settings S3Settings
	if err := json.Unmarshal(prepared, &settings); err != nil {
		return nil, err
	}
	if settings.Endpoint == "" {
		return nil, fmt.Errorf("oss region or endpoint is required")
	}
	return newS3(prepared, false)
}

func newS3(raw json.RawMessage, qiniu bool) (Store, error) {
	var settings S3Settings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Bucket == "" || settings.AccessKey == "" || settings.SecretKey == "" {
		return nil, fmt.Errorf("bucket, accessKey and secretKey are required")
	}
	if qiniu && settings.Endpoint == "" {
		endpoint, ok := qiniuEndpoints[settings.Region]
		if !ok {
			return nil, fmt.Errorf("qiniu region %q is not known; set endpoint", settings.Region)
		}
		settings.Endpoint = "https://" + endpoint
	}
	if settings.Region == "" {
		settings.Region = "us-east-1"
	}
	host, secure, err := splitEndpoint(settings.Endpoint)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupPath
	if settings.VirtualHost {
		lookup = minio.BucketLookupDNS
	}
	opts := &minio.Options{
		Creds:        credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, ""),
		Secure:       secure,
		Region:       settings.Region,
		BucketLookup: lookup,
	}
	if settings.SkipTLS {
		opts.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	core, err := minio.NewCore(host, opts)
	if err != nil {
		return nil, err
	}
	return &s3Store{settings: settings, qiniu: qiniu, core: core, layout: Layout{}.normalize()}, nil
}

func splitEndpoint(raw string) (string, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return "s3.amazonaws.com", true, nil
	}
	if !strings.Contains(raw, "://") {
		return raw, true, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	return parsed.Host, parsed.Scheme != "http", nil
}

func (s *s3Store) key(name string) string { return joinKey(s.settings.Prefix, name) }

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader) error {
	return WriteChunked(ctx, s3Blob{s}, s.key(key), r, s.layout)
}

func (s *s3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return OpenChunked(ctx, s3Blob{s}, s.key(key))
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	base := s.key(key)
	var extra []string
	for object := range s.core.Client.ListObjects(ctx, s.settings.Bucket, minio.ListObjectsOptions{Prefix: base, Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		extra = append(extra, object.Key)
	}
	for upload := range s.core.ListIncompleteUploads(ctx, s.settings.Bucket, base, true) {
		if upload.Err != nil {
			continue
		}
		_ = s.core.AbortMultipartUpload(ctx, s.settings.Bucket, upload.Key, upload.UploadID)
	}
	return RemoveChunked(ctx, s3Blob{s}, base, extra)
}

func (s *s3Store) Exists(ctx context.Context, key string) (bool, error) {
	base := s.key(key)
	if _, err := s.core.StatObject(ctx, s.settings.Bucket, base+manifestSuffix, minio.StatObjectOptions{}); err == nil {
		return true, nil
	}
	_, err := s.core.StatObject(ctx, s.settings.Bucket, base, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	var response minio.ErrorResponse
	if errors.As(err, &response) && (response.Code == "NoSuchKey" || response.StatusCode == http.StatusNotFound) {
		return false, nil
	}
	return false, err
}

func (s *s3Store) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return testObject(ctx, s)
}

// Target exposes the settings the archive uploader and DuckDB need.
func (s *s3Store) Target() S3Settings {
	settings := s.settings
	if s.qiniu && !settings.VirtualHost {
		settings.VirtualHost = false
	}
	return settings
}

type s3Blob struct{ store *s3Store }

func (b s3Blob) options() minio.PutObjectOptions {
	return minio.PutObjectOptions{StorageClass: b.store.settings.StorageClass}
}

func (b s3Blob) Put(ctx context.Context, key string, data []byte, sendMD5 bool) error {
	opts := b.options()
	opts.SendContentMd5 = sendMD5
	_, err := b.store.core.PutObject(ctx, b.store.settings.Bucket, key, bytes.NewReader(data), int64(len(data)), "", "", opts)
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

func (b s3Blob) Start(ctx context.Context, key string) (string, error) {
	return b.store.core.NewMultipartUpload(ctx, b.store.settings.Bucket, key, b.options())
}

func (b s3Blob) PutPart(ctx context.Context, key, uploadID string, partNumber int, data []byte) (string, error) {
	sum := md5.Sum(data)
	part, err := b.store.core.PutObjectPart(ctx, b.store.settings.Bucket, key, uploadID, partNumber, bytes.NewReader(data), int64(len(data)), minio.PutObjectPartOptions{
		Md5Base64: base64.StdEncoding.EncodeToString(sum[:]),
	})
	if err != nil {
		return "", fmt.Errorf("upload part %d of %s: %w", partNumber, key, err)
	}
	return part.ETag, nil
}

func (b s3Blob) Complete(ctx context.Context, key, uploadID string, parts []Part) error {
	completed := make([]minio.CompletePart, len(parts))
	for i, part := range parts {
		completed[i] = minio.CompletePart{PartNumber: part.Number, ETag: part.ETag}
	}
	_, err := b.store.core.CompleteMultipartUpload(ctx, b.store.settings.Bucket, key, uploadID, completed, b.options())
	return err
}

func (b s3Blob) Abort(ctx context.Context, key, uploadID string) error {
	return b.store.core.AbortMultipartUpload(ctx, b.store.settings.Bucket, key, uploadID)
}

func (b s3Blob) Get(ctx context.Context, key string) ([]byte, error) {
	object, _, _, err := b.store.core.GetObject(ctx, b.store.settings.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return io.ReadAll(object)
}

func (b s3Blob) Remove(ctx context.Context, key string) error {
	err := b.store.core.RemoveObject(ctx, b.store.settings.Bucket, key, minio.RemoveObjectOptions{})
	if err == nil {
		return nil
	}
	var response minio.ErrorResponse
	if errors.As(err, &response) && (response.Code == "NoSuchKey" || response.StatusCode == http.StatusNotFound) {
		return nil
	}
	return err
}

// AsS3 reports whether this store speaks the S3 API, including Qiniu.
func AsS3(store Store) (S3Settings, bool) {
	s3, ok := store.(*s3Store)
	if !ok {
		return S3Settings{}, false
	}
	return s3.Target(), true
}
