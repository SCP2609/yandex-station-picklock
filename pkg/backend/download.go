package backend

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
)

type byteRange struct{ start, end int64 }

// DownloadMonitor tracks complete HTTP responses, including resumed downloads.
// It confirms bytes sent by the server; the user's confirmation remains separate.
type DownloadMonitor struct {
	path     string
	size     int64
	mu       sync.Mutex
	ranges   []byteRange
	done     chan struct{}
	complete bool
}

func NewDownloadMonitor(path string, size int64) *DownloadMonitor {
	return &DownloadMonitor{path: path, size: size, done: make(chan struct{})}
}

func (m *DownloadMonitor) Wait(ctx context.Context) error {
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *DownloadMonitor) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != m.path {
			next.ServeHTTP(w, r)
			return
		}
		response := &transferResponse{ResponseWriter: w}
		next.ServeHTTP(response, r)
		if response.failed || r.Context().Err() != nil {
			return
		}
		length, err := strconv.ParseInt(response.Header().Get("Content-Length"), 10, 64)
		if err != nil || response.written != length {
			return
		}
		start, end := int64(0), m.size-1
		switch response.status {
		case http.StatusOK:
			if length != m.size {
				return
			}
		case http.StatusPartialContent:
			var total int64
			header := response.Header().Get("Content-Range")
			if _, err := fmt.Sscanf(header, "bytes %d-%d/%d", &start, &end, &total); err != nil || total != m.size || header != fmt.Sprintf("bytes %d-%d/%d", start, end, total) || length != end-start+1 {
				return
			}
		default:
			return
		}
		m.record(start, end+1)
	})
}

func (m *DownloadMonitor) record(start, end int64) {
	if start < 0 || end <= start || end > m.size {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.complete {
		return
	}
	m.ranges = append(m.ranges, byteRange{start, end})
	sort.Slice(m.ranges, func(i, j int) bool { return m.ranges[i].start < m.ranges[j].start })
	merged := m.ranges[:0]
	for _, span := range m.ranges {
		if len(merged) == 0 || merged[len(merged)-1].end < span.start {
			merged = append(merged, span)
		} else {
			merged[len(merged)-1].end = max(merged[len(merged)-1].end, span.end)
		}
	}
	m.ranges = merged
	// Bound fragmented, attacker-controlled Range bookkeeping. Forgetting coverage
	// is conservative: a later complete transfer can still finish the update.
	if len(m.ranges) > 4096 {
		m.ranges = nil
		return
	}
	if len(merged) == 1 && merged[0].start == 0 && merged[0].end == m.size {
		m.complete = true
		close(m.done)
	}
}

type transferResponse struct {
	http.ResponseWriter
	status  int
	written int64
	failed  bool
}

func (w *transferResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *transferResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.written += int64(n)
	if err != nil || n != len(body) {
		w.failed = true
	}
	return n, err
}

func (w *transferResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
