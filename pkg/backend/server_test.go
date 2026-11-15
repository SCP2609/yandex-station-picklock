package backend_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend"
	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

// net.Pipe exercises the real HTTP server even when OS sockets are unavailable.
type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}
}

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.connections <- server:
		return client, nil
	case <-l.closed:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		_ = client.Close()
		_ = server.Close()
		return nil, ctx.Err()
	}
}

func testClient(t *testing.T, listener *pipeListener) *http.Client {
	t.Helper()
	transport := &http.Transport{DialContext: listener.dial, DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Timeout: 3 * time.Second, Transport: transport}
}

func startServer(t *testing.T, handler http.Handler) (*backend.Server, *pipeListener, <-chan error) {
	t.Helper()
	listener := &pipeListener{connections: make(chan net.Conn), closed: make(chan struct{})}
	s, err := backend.NewServerBuilder().WithAddress(listener.Addr().String()).WithHandler(handler).Build()
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- s.Serve(listener) }()
	t.Cleanup(func() { _ = s.Close() })
	return s, listener, result
}

func stopServer(t *testing.T, server *backend.Server, result <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not stop")
	}
}

func get(t *testing.T, client *http.Client, address, path string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest("GET", "http://"+address+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/ota/") {
		r.Host = "quasar.yandex.net"
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, body
}

func TestRestartWithNewHandlerDisablesOTA(t *testing.T) {
	update, _, data := updateFixture(t)
	server, listener, result := startServer(t, backend.NewRouter(update))
	address := listener.Addr().String()
	client := testClient(t, listener)
	_, body := get(t, client, address, "/check_updates?version=factory")
	var manifest ota.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil || !manifest.HasUpdate {
		t.Fatalf("initial manifest: %s, error: %v", body, err)
	}
	if status, body := get(t, client, address, imagePath); status != 200 || string(body) != string(data) {
		t.Fatalf("initial download: status=%d, body=%s", status, body)
	}
	stopServer(t, server, result)

	server, listener, result = startServer(t, backend.NewRouter(nil))
	client = testClient(t, listener)
	if status, body := get(t, client, address, "/check_updates?version=factory"); status != 200 || string(body) != `{"hasUpdate":false}` {
		t.Fatalf("manifest after restart: status=%d, body=%s", status, body)
	}
	if status, _ := get(t, client, address, imagePath); status != 404 {
		t.Fatalf("image still available after restart: status=%d", status)
	}
	stopServer(t, server, result)
}

func TestShutdownWaitsForActiveHandler(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("completed"))
	})
	server, listener, result := startServer(t, handler)
	client := testClient(t, listener)
	clientResult := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		clientResult <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("handler did not start")
	}
	shutdown := make(chan error, 1)
	go func() { shutdown <- server.Shutdown(ctx) }()
	select {
	case err := <-shutdown:
		t.Fatalf("shutdown returned while handler was active: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	for _, completion := range []<-chan error{shutdown, clientResult, result} {
		select {
		case err := <-completion:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("shutdown did not finish")
		}
	}
}

func TestServerBuilderValidation(t *testing.T) {
	if _, err := backend.NewServerBuilder().Build(); err == nil {
		t.Fatal("accepted a missing handler")
	}
	if _, err := backend.NewServerBuilder().WithHandler(backend.NewRouter(nil)).WithAddress("invalid").Build(); err == nil {
		t.Fatal("accepted an invalid address")
	}
}
