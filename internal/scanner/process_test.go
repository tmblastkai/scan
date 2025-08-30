package scanner

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestProcessNetworkIdlePrunesLongRequests verifies that a long-polling request
// does not block the network idle detection indefinitely.
func TestProcessNetworkIdlePrunesLongRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, "<!doctype html><script>fetch('/slow');</script>"); err != nil {
				t.Fatalf("write html: %v", err)
			}
		case "/slow":
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	hostPort := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.Split(hostPort, ":")
	rec := InputRecord{Host: parts[0], Port: parts[1], Protocol: "http", Raw: []string{parts[0], parts[1]}}

	res := Process(rec, 3*time.Second)
	if res.Error != "" {
		t.Fatalf("process returned error: %v", res.Error)
	}
}

// TestProcessSetsPassTestForSuccessfulCode ensures that a successful response
// (status <= 400) marks the result as passing even without login keywords or
// allow-list matches.
func TestProcessSetsPassTestForSuccessfulCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, "<html><body>ok</body></html>"); err != nil {
			t.Fatalf("write html: %v", err)
		}
	}))
	defer srv.Close()

	hostPort := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.Split(hostPort, ":")
	rec := InputRecord{Host: parts[0], Port: parts[1], Protocol: "http", Raw: []string{parts[0], parts[1]}}

	res := Process(rec, 3*time.Second)
	if res.ResponseCode != 200 {
		t.Fatalf("expected response code 200, got %d", res.ResponseCode)
	}
	if !res.PassTest {
		t.Fatalf("expected pass_test true for response code %d", res.ResponseCode)
	}
}
