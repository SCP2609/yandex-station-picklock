package backend

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server owns one serving cycle. Build a new instance after Shutdown.
type Server struct {
	http *http.Server
}

type ServerBuilder struct {
	address   string
	handler   http.Handler
	tlsConfig *tls.Config
}

func NewServerBuilder() *ServerBuilder {
	return &ServerBuilder{address: ":443"}
}

func (b *ServerBuilder) WithAddress(address string) *ServerBuilder {
	b.address = address
	return b
}

func (b *ServerBuilder) WithHandler(handler http.Handler) *ServerBuilder {
	b.handler = handler
	return b
}

// Certificates remain caller-supplied; private TLS keys are never embedded.
func (b *ServerBuilder) WithTLSConfig(config *tls.Config) *ServerBuilder {
	if config != nil {
		b.tlsConfig = config.Clone()
	} else {
		b.tlsConfig = nil
	}
	return b
}

func (b *ServerBuilder) Build() (*Server, error) {
	if b.handler == nil {
		return nil, errors.New("backend handler is required")
	}
	if _, _, err := net.SplitHostPort(b.address); err != nil {
		return nil, fmt.Errorf("backend address: %w", err)
	}
	return &Server{http: &http.Server{
		Addr:              b.address,
		Handler:           b.handler,
		TLSConfig:         b.tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       time.Minute,
		// Firmware transfers can exceed a fixed write timeout.
	}}, nil
}

// ListenAndServe blocks; the caller owns the goroutine and handles its result.
func (s *Server) ListenAndServe() error {
	return serveError(s.http.ListenAndServe())
}

// Serve takes ownership of listener, including closing it on shutdown.
func (s *Server) Serve(listener net.Listener) error {
	return serveError(s.http.Serve(listener))
}

func (s *Server) ServeTLS(listener net.Listener) error {
	return serveError(s.http.ServeTLS(listener, "", ""))
}

// Wait for Shutdown to return before starting the next server on the same port.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Close aborts active transfers; use only if graceful shutdown cannot finish.
func (s *Server) Close() error {
	return s.http.Close()
}

func serveError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
