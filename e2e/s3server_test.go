//go:build e2e

package e2e

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// s3Server is a small S3-compatible endpoint. It keeps objects in memory and
// answers the requests the AWS SDK uploader, the MD5 reread and DuckDB httpfs
// make, so the storage prefix path can be proven without a bucket service.
type s3Server struct {
	mu       sync.Mutex
	objects  map[string]s3Object
	requests []string
	server   *httptest.Server
}

// s3Object is one stored object. The modification time belongs to the object,
// so the listing and the read report the same one.
type s3Object struct {
	body       []byte
	modifiedAt time.Time
}

func newS3Server(t *testing.T) *s3Server {
	s := &s3Server{objects: map[string]s3Object{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *s3Server) endpoint() string { return s.server.URL }

// trace lists the requests the endpoint answered, so a failing test can show
// what the client actually asked for.
func (s *s3Server) trace() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// keys lists the object keys inside a bucket.
func (s *s3Server) keys(bucket string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.objects))
	for name := range s.objects {
		if key, ok := strings.CutPrefix(name, bucket+"/"); ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func (s *s3Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	s.mu.Unlock()
	bucket, key := splitObjectPath(r.URL.Path)
	switch {
	case bucket == "":
		http.Error(w, "bucket is required", http.StatusNotFound)
	case key == "" && r.Method == http.MethodGet:
		s.listBucket(w, r, bucket)
	case key == "" && r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case key != "" && r.Method == http.MethodPut:
		s.putObject(w, r, bucket+"/"+key)
	case key != "" && r.Method == http.MethodGet:
		s.getObject(w, bucket+"/"+key, true)
	case key != "" && r.Method == http.MethodHead:
		s.getObject(w, bucket+"/"+key, false)
	case key != "" && r.Method == http.MethodDelete:
		s.deleteObject(w, bucket+"/"+key)
	default:
		http.Error(w, "unsupported request", http.StatusNotFound)
	}
}

func splitObjectPath(path string) (bucket, key string) {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return "", ""
	}
	bucket, key, _ = strings.Cut(trimmed, "/")
	return bucket, key
}

func (s *s3Server) putObject(w http.ResponseWriter, r *http.Request, name string) {
	body, err := readObjectBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.objects[name] = s3Object{body: body, modifiedAt: time.Now().UTC().Truncate(time.Second)}
	s.mu.Unlock()
	sum := md5.Sum(body)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.WriteHeader(http.StatusOK)
}

func (s *s3Server) getObject(w http.ResponseWriter, name string, withBody bool) {
	s.mu.Lock()
	object, ok := s.objects[name]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "NoSuchKey", http.StatusNotFound)
		return
	}
	sum := md5.Sum(object.body)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(object.body)))
	w.Header().Set("Last-Modified", object.modifiedAt.Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	if withBody {
		_, _ = w.Write(object.body)
	}
}

func (s *s3Server) deleteObject(w http.ResponseWriter, name string) {
	s.mu.Lock()
	delete(s.objects, name)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// listBucket answers the bucket listing the SDK and DuckDB httpfs use.
func (s *s3Server) listBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	query := r.URL.Query()
	if query.Has("location") {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
			`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return
	}
	prefix := query.Get("prefix")
	delimiter := query.Get("delimiter")
	encoded := query.Get("encoding-type") == "url"

	s.mu.Lock()
	names := make([]string, 0, len(s.objects))
	for name := range s.objects {
		if strings.HasPrefix(name, bucket+"/") && strings.HasPrefix(strings.TrimPrefix(name, bucket+"/"), prefix) {
			names = append(names, name)
		}
	}
	s.mu.Unlock()
	sort.Strings(names)

	result := listBucketResult{
		XMLNS:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		Prefix:      prefix,
		MaxKeys:     1000,
		IsTruncated: false,
	}
	seen := map[string]bool{}
	for _, name := range names {
		key := strings.TrimPrefix(name, bucket+"/")
		s.mu.Lock()
		object := s.objects[name]
		s.mu.Unlock()
		size := len(object.body)
		if delimiter != "" {
			if rest := strings.TrimPrefix(key, prefix); strings.Contains(rest, delimiter) {
				common := prefix + rest[:strings.Index(rest, delimiter)+len(delimiter)]
				if !seen[common] {
					seen[common] = true
					result.CommonPrefixes = append(result.CommonPrefixes, listCommonPrefix{Prefix: common})
				}
				continue
			}
		}
		result.Contents = append(result.Contents, listContent{
			Key:          key,
			LastModified: object.modifiedAt.Format("2006-01-02T15:04:05.000Z"),
			ETag:         `"` + etagOf(s, name) + `"`,
			Size:         size,
			StorageClass: "STANDARD",
		})
	}
	result.KeyCount = len(result.Contents)
	if encoded {
		result.EncodingType = "url"
		for i := range result.Contents {
			result.Contents[i].Key = url.QueryEscape(result.Contents[i].Key)
		}
		for i := range result.CommonPrefixes {
			result.CommonPrefixes[i].Prefix = url.QueryEscape(result.CommonPrefixes[i].Prefix)
		}
	}
	payload, err := xml.Marshal(result)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(payload)
}

// etagOf is the object digest, the same value the object read returns.
func etagOf(s *s3Server, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := md5.Sum(s.objects[name].body)
	return hex.EncodeToString(sum[:])
}

// readObjectBody reads a PutObject body. The SDK streams a PutObject with
// aws-chunked framing, so that framing is undone here.
func readObjectBody(r *http.Request) ([]byte, error) {
	if r.Header.Get("Content-Encoding") != "aws-chunked" && r.Header.Get("x-amz-decoded-content-length") == "" {
		return io.ReadAll(r.Body)
	}
	reader := bufio.NewReader(r.Body)
	var out []byte
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header := strings.TrimSpace(line)
		if header == "" {
			continue
		}
		sizeText := header
		if i := strings.IndexByte(header, ';'); i >= 0 {
			sizeText = header[:i]
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeText), 16, 64)
		if err != nil {
			return nil, fmt.Errorf("chunk header %q: %w", header, err)
		}
		if size == 0 {
			return out, nil
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := reader.Discard(2); err != nil && err != io.EOF {
			return nil, err
		}
	}
}

type listBucketResult struct {
	XMLName        xml.Name           `xml:"ListBucketResult"`
	XMLNS          string             `xml:"xmlns,attr"`
	Name           string             `xml:"Name"`
	Prefix         string             `xml:"Prefix"`
	Marker         string             `xml:"Marker,omitempty"`
	NextMarker     string             `xml:"NextMarker,omitempty"`
	KeyCount       int                `xml:"KeyCount"`
	MaxKeys        int                `xml:"MaxKeys"`
	Delimiter      string             `xml:"Delimiter,omitempty"`
	EncodingType   string             `xml:"EncodingType,omitempty"`
	IsTruncated    bool               `xml:"IsTruncated"`
	Contents       []listContent      `xml:"Contents"`
	CommonPrefixes []listCommonPrefix `xml:"CommonPrefixes"`
}

type listContent struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int    `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type listCommonPrefix struct {
	Prefix string `xml:"Prefix"`
}
