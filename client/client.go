// Package client fetches IDs from idserver nodes in batches.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
)

// batchSize is how many IDs one request asks for, so most NextID calls make no network call.
const batchSize = 100

var (
	ErrNoNodes        = errors.New("client: no nodes configured")
	ErrAllNodesFailed = errors.New("client: all nodes failed")
)

// Client serves IDs from a local buffer and refills it from the nodes.
type Client struct {
	nodes []string // base URLs, for example "http://localhost:8081"

	mu  sync.Mutex // guards buf and cur; also held during a refill so only one request is in flight
	buf []uint64
	cur int // index of the node that worked last, tried first next time
}

// New returns a Client for the given node base URLs.
func New(nodes []string) *Client { return &Client{nodes: nodes} }

// NextID returns one ID, fetching a new batch when the buffer is empty.
func (c *Client) NextID(ctx context.Context) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.buf) == 0 {
		if err := c.refill(ctx); err != nil {
			return 0, err
		}
	}
	id := c.buf[0]
	c.buf = c.buf[1:]
	return id, nil
}

// refill asks each node in turn, starting at the last good one, until one answers.
func (c *Client) refill(ctx context.Context) error {
	if len(c.nodes) == 0 {
		return ErrNoNodes
	}
	var last error
	for i := range c.nodes {
		idx := (c.cur + i) % len(c.nodes)
		ids, err := fetch(ctx, c.nodes[idx])
		if err == nil {
			c.cur, c.buf = idx, ids
			return nil
		}
		last = err
	}
	return fmt.Errorf("%w: %w", ErrAllNodesFailed, last)
}

// fetch requests one batch from a single node.
func fetch(ctx context.Context, node string) ([]uint64, error) {
	url := fmt.Sprintf("%s/v1/ids?count=%d", node, batchSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d", node, resp.StatusCode)
	}

	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%s: bad response: %w", node, err)
	}
	ids := make([]uint64, 0, len(body.IDs))
	for _, s := range body.IDs {
		id, err := strconv.ParseUint(s, 10, 64) // the server sends strings, see the HTTP handler
		if err != nil {
			return nil, fmt.Errorf("%s: bad id %q: %w", node, s, err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%s: empty batch", node) // would make NextID read an empty buffer
	}
	return ids, nil
}
