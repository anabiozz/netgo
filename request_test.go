package netter

import (
	"bytes"
	"testing"
)

func TestRequest(t *testing.T) {
	_, err := NewRequest("GET", "://foo", nil)
	if err == nil {
		t.Fatal("should error")
	}

	_, err = NewRequest("GET", "http://foo", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	body := bytes.NewReader([]byte("yo"))
	req, err := NewRequest("GET", "/", body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	req.Header.Set("X-Test", "foo")
	if v, ok := req.Header["X-Test"]; !ok || len(v) != 1 || v[0] != "foo" {
		t.Fatalf("bad headers: %v", req.Header)
	}

	if req.ContentLength != 2 {
		t.Fatalf("bad ContentLength: %d", req.ContentLength)
	}
}
