package client_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abhi-8849/idgen1/client"
)

// batchServer answers /v1/ids with 100 sequential ids and counts how many requests it got.
func batchServer(start int, hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		ids := make([]string, 100)
		for i := range ids {
			ids[i] = fmt.Sprintf("\"%d\"", start+i)
		}
		fmt.Fprintf(w, `{"ids":[%s]}`, strings.Join(ids, ","))
	}))
}

func TestOneFetchServesManyIDs(t *testing.T) {
	var hits atomic.Int32
	srv := batchServer(1, &hits)
	defer srv.Close()
	c := client.New([]string{srv.URL})
	for want := uint64(1); want <= 100; want++ {
		if got, err := c.NextID(context.Background()); err != nil || got != want {
			t.Fatalf("want %d, got %d (err %v)", want, got, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("100 ids should need 1 request, got %d", hits.Load())
	}
	c.NextID(context.Background()) // the 101st id forces a second fetch
	if hits.Load() != 2 {
		t.Fatalf("want a refill, got %d requests", hits.Load())
	}
}

func TestFailsOverToNextNode(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	var hits atomic.Int32
	good := batchServer(500, &hits)
	defer good.Close()

	c := client.New([]string{bad.URL, good.URL})
	if got, err := c.NextID(context.Background()); err != nil || got != 500 {
		t.Fatalf("want 500 from the second node, got %d (err %v)", got, err)
	}
}

func TestAllNodesFailed(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	if _, err := client.New([]string{bad.URL}).NextID(context.Background()); !errors.Is(err, client.ErrAllNodesFailed) {
		t.Fatalf("want ErrAllNodesFailed, got %v", err)
	}
}
