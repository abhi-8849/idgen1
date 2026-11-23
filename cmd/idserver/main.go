// Command idserver runs the ID generator as an HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abhi-8849/idgen1/server"
	"github.com/abhi-8849/idgen1/snowflake"
	"github.com/abhi-8849/idgen1/workerid"
)

func main() {
	if err := run(); err != nil {
		slog.Error("idserver failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	dc := flag.Uint("dc", 0, "datacenter id (0-31)")
	worker := flag.Uint("worker", 0, "worker id (0-31)")
	flag.Parse()

	// ctx is cancelled by SIGINT or SIGTERM; it is our "please shut down" signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lease, err := workerid.NewStatic(uint8(*dc), uint8(*worker)).Acquire(ctx)
	if err != nil {
		return err
	}
	// Release on every exit path; it gets a fresh ctx because ctx is already cancelled on shutdown.
	defer lease.Release(context.Background())

	gen, err := snowflake.New(snowflake.Config{DC: lease.DC(), Worker: lease.Worker(), Lost: lease.Done()}, nil)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: *addr, Handler: server.New(gen, lease.Done()).Handler()}
	return serve(ctx, srv)
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
