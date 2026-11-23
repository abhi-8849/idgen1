// Command idserver runs the ID generator as an HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abhi-8849/idgen1/server"
	"github.com/abhi-8849/idgen1/snowflake"
	"github.com/abhi-8849/idgen1/workerid"
	"github.com/abhi-8849/idgen1/workerid/etcd"
)

// TODO: add tests for flag handling and shutdown, and an end-to-end check of the Docker Compose and Kubernetes deployments.
func main() {
	if err := run(); err != nil {
		slog.Error("idserver failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	dc := flag.Uint("dc", 0, "datacenter id (0-31)")
	worker := flag.Uint("worker", 0, "worker id (0-31); only used with -allocator static")
	kind := flag.String("allocator", "static", "worker id source: static or etcd")
	endpoint := flag.String("etcd", "localhost:2379", "etcd endpoint; only used with -allocator etcd")
	flag.Parse()

	// ctx is cancelled by SIGINT or SIGTERM; it is our "please shut down" signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	alloc, err := newAllocator(*kind, *endpoint, uint8(*dc), uint8(*worker))
	if err != nil {
		return err
	}
	lease, err := alloc.Acquire(ctx)
	if err != nil {
		return err
	}
	// Release on every exit path; it gets a fresh ctx because ctx is already cancelled on shutdown.
	defer lease.Release(context.Background())

	log := slog.Default()
	metrics := server.NewMetrics(log)
	cfg := snowflake.Config{DC: lease.DC(), Worker: lease.Worker(), Lost: lease.Done(), Hooks: metrics}
	gen, err := snowflake.New(cfg, nil)
	if err != nil {
		return err
	}
	go logLeaseLost(ctx, lease.Done(), log)

	srv := &http.Server{Addr: *addr, Handler: server.New(gen, lease.Done(), metrics, log).Handler()}
	log.Info("server starting", "addr", *addr, "dc", lease.DC(), "worker", lease.Worker())
	err = serve(ctx, srv)
	log.Info("server stopped")
	return err
}

// logLeaseLost runs in its own goroutine, started by run, and ends after logging once or when ctx ends.
func logLeaseLost(ctx context.Context, lost <-chan struct{}, log *slog.Logger) {
	select {
	case <-lost: // closed by the lease owner: the worker id is gone and the generator now refuses IDs
		log.Error("lease lost")
	case <-ctx.Done(): // normal shutdown: Release also closes lost, but that is not a failure, so stay quiet
	}
}

// newAllocator picks where the worker id comes from.
func newAllocator(kind, endpoint string, dc, worker uint8) (workerid.Allocator, error) {
	switch kind {
	case "static":
		return workerid.NewStatic(dc, worker), nil
	case "etcd":
		return etcd.New([]string{endpoint}, dc)
	}
	return nil, fmt.Errorf("unknown -allocator %q (want static or etcd)", kind)
}

// serve runs srv until it fails or ctx is cancelled, then drains in-flight requests.
func serve(ctx context.Context, srv *http.Server) error {
	// errCh carries ListenAndServe's result out of its goroutine. Buffered (1) so the
	// goroutine can send and exit even if nobody is receiving any more.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }() // started here; ends when Shutdown is called or listen fails

	select {
	case err := <-errCh: // the server stopped on its own (e.g. port in use)
		return err
	case <-ctx.Done(): // signal received: stop accepting and drain
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return err
	}
	// ListenAndServe returns http.ErrServerClosed after a clean Shutdown; that is not a failure.
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
