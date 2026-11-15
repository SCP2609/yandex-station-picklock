package backend

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/fr33dman/yandex-station-picklock/pkg/backend/ota"
)

type ListenerConfig struct {
	HTTPAddress  string
	HTTPSAddress string
	TLSConfig    *tls.Config
}

// Controller owns graceful replacement of all HTTP/TLS listeners as one unit.
type Controller struct {
	config  ListenerConfig
	mu      sync.Mutex
	current *backendRun
}

type backendRun struct {
	servers  []*Server
	done     chan struct{}
	failed   chan error
	results  []error
	download *DownloadMonitor
}

func NewController(config ListenerConfig) (*Controller, error) {
	if config.HTTPAddress == "" {
		return nil, errors.New("HTTP address is required for local firmware downloads")
	}
	if config.HTTPSAddress != "" && config.TLSConfig == nil {
		return nil, errors.New("HTTPS requires a caller-supplied TLS configuration")
	}
	if config.TLSConfig != nil {
		config.TLSConfig = config.TLSConfig.Clone()
	}
	return &Controller{config: config}, nil
}

func (c *Controller) Replace(ctx context.Context, update *ota.Update) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.stop(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	handler := NewRouter(update)
	run := &backendRun{done: make(chan struct{}), failed: make(chan error, 1)}
	if update != nil {
		image, err := update.Open()
		if err != nil {
			return err
		}
		size, seekErr := image.Seek(0, io.SeekEnd)
		closeErr := image.Close()
		if err := errors.Join(seekErr, closeErr); err != nil {
			return err
		}
		if size <= 0 {
			return errors.New("empty OTA image")
		}
		run.download = NewDownloadMonitor(update.DownloadPath(), size)
		handler = run.download.Handler(handler)
	}
	type endpoint struct {
		address string
		tls     bool
	}
	endpoints := []endpoint{{c.config.HTTPAddress, false}}
	if c.config.HTTPSAddress != "" {
		endpoints = append(endpoints, endpoint{c.config.HTTPSAddress, true})
	}
	var listeners []net.Listener
	// Reserve every listener before starting any serving goroutine.
	for _, endpoint := range endpoints {
		server, err := NewServerBuilder().WithAddress(endpoint.address).WithHandler(handler).WithTLSConfig(c.config.TLSConfig).Build()
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return err
		}
		listener, err := net.Listen("tcp", endpoint.address)
		if err != nil {
			for _, l := range listeners {
				_ = l.Close()
			}
			return fmt.Errorf("listen backend: %w", err)
		}
		listeners = append(listeners, listener)
		run.servers = append(run.servers, server)
	}
	run.results = make([]error, len(run.servers))
	var wg sync.WaitGroup
	for i, server := range run.servers {
		wg.Go(func() {
			var err error
			if endpoints[i].tls {
				err = server.ServeTLS(listeners[i])
			} else {
				err = server.Serve(listeners[i])
			}
			run.results[i] = err
			if err != nil {
				select {
				case run.failed <- err:
				default:
				}
			}
		})
	}
	go func() { wg.Wait(); close(run.done) }()
	c.current = run
	return nil
}

func (c *Controller) WaitDownload(ctx context.Context) error {
	c.mu.Lock()
	run := c.current
	c.mu.Unlock()
	if run == nil || run.download == nil {
		return errors.New("OTA backend is not running")
	}
	select {
	case <-run.download.done:
		return nil
	case err := <-run.failed:
		return err
	case <-run.done:
		return errors.New("backend stopped before the download completed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait reports serving failures even when OTA is disabled (standalone mode).
func (c *Controller) Wait(ctx context.Context) error {
	c.mu.Lock()
	run := c.current
	c.mu.Unlock()
	if run == nil {
		return errors.New("backend is not running")
	}
	select {
	case err := <-run.failed:
		return err
	case <-run.done:
		return errors.Join(run.results...)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Controller) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stop(ctx)
}

func (c *Controller) stop(ctx context.Context) error {
	if c.current == nil {
		return nil
	}
	run := c.current
	c.current = nil
	failures := make([]error, len(run.servers))
	var wg sync.WaitGroup
	for i, server := range run.servers {
		wg.Go(func() {
			if err := server.Shutdown(ctx); err != nil {
				failures[i] = errors.Join(err, server.Close())
			}
		})
	}
	wg.Wait()
	<-run.done
	return errors.Join(append(failures, run.results...)...)
}
