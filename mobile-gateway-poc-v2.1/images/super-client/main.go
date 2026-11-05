// client connects (over WebSocket, through ONE region's proxy) and verifies the
// full geo-design behavior from that entry point:
//
//   1. REST (/api/*)              → reaches that region's REST service (HTTP).
//   2. Request/reply to LOCAL region's worker  (no supercluster hop needed).
//   3. Request/reply to REMOTE region's worker (forwarded via the supercluster).
//   4. Receives LOCAL region's room events     (trivial / same-cluster).
//   5. Receives REMOTE region's room events    (forwarded via the supercluster).
//
// Run the SAME client against each proxy (region-a entry, then region-b entry) to
// prove both proxies work and the supercluster federates BOTH directions.
//
// Env:
//   REST_BASE_URL   e.g. http://proxy-region-a:8080
//   NATS_WS_URL     e.g. ws://proxy-region-a:8080/nats
//   LOCAL_REGION    the region this proxy/NATS belongs to  (e.g. region-a)
//   REMOTE_REGION   the other region                        (e.g. region-b)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	restBase := envOr("REST_BASE_URL", "http://proxy-region-a:8080")
	natsWSURL := envOr("NATS_WS_URL", "ws://proxy-region-a:8080/nats")
	localRegion := envOr("LOCAL_REGION", "region-a")
	remoteRegion := envOr("REMOTE_REGION", "region-b")
	eventWait := envDurationOr("EVENT_WAIT", 12*time.Second)

	log.Printf("=== client entry: proxy=%s  local=%s  remote=%s ===", natsWSURL, localRegion, remoteRegion)
	failures := 0

	// 1) REST via the proxy.
	if err := callREST(restBase); err != nil {
		log.Printf("[REST       ] FAIL: %v", err)
		failures++
	}

	nc, err := nats.Connect(natsWSURL, nats.Name("client-"+localRegion), nats.Timeout(10*time.Second))
	if err != nil {
		log.Fatalf("client: connect %q failed: %v", natsWSURL, err)
	}
	defer nc.Close()
	log.Printf("client: connected to %s", natsWSURL)

	// 2) Request/reply to LOCAL region's worker.
	if err := reqReply(nc, "demo."+localRegion+".request"); err != nil {
		log.Printf("[REQ local  ] FAIL: %v", err)
		failures++
	}
	// 3) Request/reply to REMOTE region's worker (via supercluster).
	if err := reqReply(nc, "demo."+remoteRegion+".request"); err != nil {
		log.Printf("[REQ remote ] FAIL: %v", err)
		failures++
	}

	// 4) Receive LOCAL region room events (trivial/same-cluster).
	if err := expectEvent(nc, "demo.room."+localRegion+".event", eventWait); err != nil {
		log.Printf("[SUB local  ] FAIL: %v", err)
		failures++
	}
	// 5) Receive REMOTE region room events (via supercluster).
	if err := expectEvent(nc, "demo.room."+remoteRegion+".event", eventWait); err != nil {
		log.Printf("[SUB remote ] FAIL: %v", err)
		failures++
	}

	if failures > 0 {
		log.Fatalf("client(%s entry): %d check(s) FAILED", localRegion, failures)
	}
	log.Printf("client(%s entry): ALL checks passed (REST + local/remote req-reply + local/remote events)", localRegion)
}

func callREST(base string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := base + "/api/echo"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	log.Printf("[REST       ] OK   %s -> %s", url, compact(body))
	return nil
}

func reqReply(nc *nats.Conn, subject string) error {
	msg, err := nc.Request(subject, []byte(`{"ping":"hi"}`), 10*time.Second)
	if err != nil {
		return fmt.Errorf("request %q: %w", subject, err)
	}
	log.Printf("[REQ        ] OK   %s -> %s", subject, compact(msg.Data))
	return nil
}

func expectEvent(nc *nats.Conn, subject string, wait time.Duration) error {
	got := make(chan []byte, 1)
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		select {
		case got <- m.Data:
		default:
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", subject, err)
	}
	defer sub.Unsubscribe() //nolint:errcheck
	select {
	case data := <-got:
		log.Printf("[SUB        ] OK   %s -> %s", subject, compact(data))
		return nil
	case <-time.After(wait):
		return fmt.Errorf("no event on %q within %s", subject, wait)
	}
}

func compact(b []byte) string {
	var v map[string]any
	if json.Unmarshal(b, &v) == nil {
		out, _ := json.Marshal(v)
		return string(out)
	}
	return string(b)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDurationOr(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
