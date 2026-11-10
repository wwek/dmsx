package storage

import (
	"encoding/json"
	"testing"
)

func TestGCSAndOSSDefaults(t *testing.T) {
	gcs := S3Settings{}
	if err := json.Unmarshal(defaultGCS([]byte(`{"bucket":"b","accessKey":"a","secretKey":"s"}`)), &gcs); err != nil {
		t.Fatal(err)
	}
	if gcs.Endpoint != "https://storage.googleapis.com" || !gcs.VirtualHost {
		t.Fatalf("%+v", gcs)
	}
	oss := S3Settings{}
	if err := json.Unmarshal(defaultOSS([]byte(`{"bucket":"b","accessKey":"a","secretKey":"s","region":"cn-hangzhou"}`)), &oss); err != nil {
		t.Fatal(err)
	}
	if oss.Endpoint != "https://oss-cn-hangzhou.aliyuncs.com" || !oss.VirtualHost {
		t.Fatalf("%+v", oss)
	}
}

func TestSwiftObjectURL(t *testing.T) {
	got := swiftObjectURL("https://swift.example/v1/AUTH_demo/", "archives", "dms_archives/shop/orders/a.parquet")
	want := "https://swift.example/v1/AUTH_demo/archives/dms_archives/shop/orders/a.parquet"
	if got != want {
		t.Fatal(got)
	}
	if keystoneAuthURL("https://cloud.example:5000") != "https://cloud.example:5000/v3/auth/tokens" {
		t.Fatal(keystoneAuthURL("https://cloud.example:5000"))
	}
}
