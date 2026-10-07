package httpapi

import (
	"context"
	"testing"
)

func TestPublicHTTPURLRejectsLocalTargets(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080/api/crm/candidates",
		"http://localhost/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://[::1]/",
		"ftp://example.com/",
		"",
	} {
		if _, err := publicHTTPURL(context.Background(), raw); err == nil {
			t.Errorf("publicHTTPURL(%q) should be rejected", raw)
		}
	}
}

func TestPublicHTTPURLDefaultsToHTTPS(t *testing.T) {
	u, err := publicHTTPURL(context.Background(), "93.184.216.34/karriere")
	if err != nil {
		t.Fatalf("publicHTTPURL: %v", err)
	}
	if u.String() != "https://93.184.216.34/karriere" {
		t.Errorf("got %s", u)
	}
}
