package main

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

	res := process(rec, 3*time.Second)
	if res.Error != "" {
		t.Fatalf("process returned error: %v", res.Error)
	}
}
