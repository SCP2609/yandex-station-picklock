package backend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownloadRequiresCompleteCoverage(t *testing.T) {
	monitor := NewDownloadMonitor("/ota/image.zip", 10)
	handler := monitor.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "image.zip", time.Time{}, strings.NewReader("0123456789"))
	}))
	send := func(method, span string) {
		r := httptest.NewRequest(method, "/ota/image.zip", nil)
		r.Header.Set("Range", span)
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	complete := func() bool {
		select {
		case <-monitor.done:
			return true
		default:
			return false
		}
	}
	send("HEAD", "")
	send("GET", "bytes=99-")
	if complete() {
		t.Fatal("HEAD or rejected range completed the download")
	}
	send("GET", "bytes=0-4")
	send("GET", "bytes=0-4")
	if complete() {
		t.Fatal("duplicate bytes counted twice")
	}
	send("GET", "bytes=7-9")
	if complete() {
		t.Fatal("a gap in the image was ignored")
	}
	send("GET", "bytes=5-6")
	if !complete() {
		t.Fatal("resumed download did not complete")
	}
	if err := monitor.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type failedResponse struct{ header http.Header }

func (w *failedResponse) Header() http.Header     { return w.header }
func (*failedResponse) WriteHeader(int)           {}
func (*failedResponse) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func TestFailedTransferDoesNotComplete(t *testing.T) {
	monitor := NewDownloadMonitor("/ota/image.zip", 10)
	handler := monitor.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "image.zip", time.Time{}, strings.NewReader("0123456789"))
	}))
	handler.ServeHTTP(&failedResponse{header: make(http.Header)}, httptest.NewRequest("GET", "/ota/image.zip", nil))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := monitor.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("failed transfer completed: %v", err)
	}
}
