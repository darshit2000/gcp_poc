// mobile-gateway is a tiny HTTP->NATS bridge. It sits BEHIND the region's nginx
// reverse proxy and IN FRONT OF the GCP-side NATS (the leaf hub). nginx keeps
// tunneling the client's native NATS-over-WebSocket straight to the leaf NATS on
// /nats (unchanged); only the HTTP /api/* path is routed to THIS service.
//
// For each incoming HTTP request on /api/*, the gateway performs ONE synchronous
// NATS request/reply (nc.Request) over its OWN NATS connection to the GCP-side
// leaf NATS, then writes the reply back as the HTTP response. There is no custom
// correlation/identification: HTTP is request/response on one connection, and the
// NATS reply self-correlates via nats.go's built-in private _INBOX reply subject.
//
// The request subject targets a responder that lives on the intranet cluster the
// leaf lands on (a "same reply for all users" bootstrap-style service). So the
// HTTP-turned-NATS request rides the exact same proven path as every other NATS
// request: gateway -> (leaf) -> intranet core -> responder -> reply back.
//
// Env:
//   LISTEN_ADDR    HTTP listen address              (default :8080)
//   NATS_URL       GCP-side leaf NATS URL            (default nats://nats-gcp-a:4222)
//   REQUEST_SUBJECT  subject for the bootstrap RPC   (default demo.bootstrap.request)
//   REQUEST_TIMEOUT  NATS request timeout            (default 5s)
//   REGION         label for logs/health             (default unknown)
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

const maxBodyBytes = 16 << 10 // 16 KiB cap on request bodies (per design doc).

func main() {
	addr := envOr("LISTEN_ADDR", ":8080")
	natsURL := envOr("NATS_URL", "nats://nats-gcp-a:4222")
	reqSubject := envOr("REQUEST_SUBJECT", "demo.bootstrap.request")
	reqTimeout := envDurationOr("REQUEST_TIMEOUT", 5*time.Second)
	region := envOr("REGION", "unknown")

	nc, err := nats.Connect(natsURL,
		nats.Name("mobile-gateway-"+region),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.Timeout(10*time.Second),
	)
	if err != nil {
		log.Fatalf("mobile-gateway(%s): connect to %q failed: %v", region, natsURL, err)
	}
	defer nc.Drain() //nolint:errcheck
	log.Printf("mobile-gateway(%s): connected to %s; bridging /api/* -> NATS %q (timeout %s)", region, natsURL, reqSubject, reqTimeout)

	var connected atomic.Bool
	connected.Store(true)
	nc.SetDisconnectErrHandler(func(_ *nats.Conn, _ error) { connected.Store(false) })
	nc.SetReconnectHandler(func(_ *nats.Conn) { connected.Store(true) })

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "region": region})
	})

	// Readiness depends on the NATS connection being up.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if connected.Load() && nc.IsConnected() {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "region": region})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "region": region})
	})

	// Every /api/* HTTP request becomes ONE NATS request/reply.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read_body", "detail": err.Error()})
			return
		}
		// We forward a small JSON describing the call so the responder can echo it
		// (stand-in for the real auth/userInfo request bodies). The POC responder
		// returns the same reply regardless, matching "same reply for all users".
		reqPayload, _ := json.Marshal(map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"body":   string(body),
		})

		msg, err := nc.Request(reqSubject, reqPayload, reqTimeout)
		if err != nil {
			// Timeout / no responders / NATS down -> 503 (per design doc).
			log.Printf("mobile-gateway(%s): NATS request %q failed: %v", region, reqSubject, err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "unavailable",
				"detail":  err.Error(),
				"subject": reqSubject,
			})
			return
		}
		// Success: write the NATS reply back as the HTTP response, as-is (JSON).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(msg.Data)
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("mobile-gateway(%s) listening on %s", region, addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("mobile-gateway(%s) failed: %v", region, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
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
