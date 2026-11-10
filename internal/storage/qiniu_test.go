package storage

import (
	"encoding/json"
	"testing"
)

// Qiniu speaks the S3 API through its own endpoint. A region without an
// endpoint has to name that endpoint, and the archive must address the
// bucket in the path, not in the host.
func TestQiniuEndpointAndPathStyle(t *testing.T) {
	for region, want := range map[string]string{
		"cn-east-1":  "https://s3-cn-east-1.qiniucs.com",
		"cn-north-1": "https://s3-cn-north-1.qiniucs.com",
		"us-north-1": "https://s3-us-north-1.qiniucs.com",
	} {
		raw := []byte(`{"bucket":"b","accessKey":"a","secretKey":"s","region":"` + region + `"}`)
		store, err := New(TypeQiniu, raw)
		if err != nil {
			t.Fatalf("%s: %v", region, err)
		}
		settings, ok := AsS3(store)
		if !ok {
			t.Fatalf("%s: qiniu is not an S3 store", region)
		}
		if settings.Endpoint != want {
			t.Fatalf("%s: endpoint %q", region, settings.Endpoint)
		}
		if settings.VirtualHost {
			t.Fatalf("%s: qiniu must address the bucket in the path", region)
		}
	}
}

func TestQiniuNeedsAKnownRegionOrAnEndpoint(t *testing.T) {
	raw := []byte(`{"bucket":"b","accessKey":"a","secretKey":"s","region":"cn-nowhere"}`)
	if _, err := New(TypeQiniu, raw); err == nil {
		t.Fatal("an unknown region without an endpoint was accepted")
	}
	raw = []byte(`{"bucket":"b","accessKey":"a","secretKey":"s","region":"cn-nowhere","endpoint":"http://127.0.0.1:9000"}`)
	store, err := New(TypeQiniu, raw)
	if err != nil {
		t.Fatal(err)
	}
	settings, ok := AsS3(store)
	if !ok || settings.Endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("%+v", settings)
	}
}

// The archive prefix is the storage prefix plus the object key, so the
// upload, the MD5 reread and the cold query name the same object.
func TestPrefixedKey(t *testing.T) {
	if got := joinKey("qi", "dms_archives/shop/orders/a.parquet"); got != "qi/dms_archives/shop/orders/a.parquet" {
		t.Fatal(got)
	}
	settings := S3Settings{}
	if err := json.Unmarshal([]byte(`{"bucket":"b","accessKey":"a","secretKey":"s","prefix":"/qi/"}`), &settings); err != nil {
		t.Fatal(err)
	}
	if got := settings.Prefix; got != "/qi/" {
		t.Fatalf("%q", got)
	}
}
