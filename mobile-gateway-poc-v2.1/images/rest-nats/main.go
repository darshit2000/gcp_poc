// rest-nats is the stand-in for the former HTTP REST services (auth / user-service)
// AFTER they are converted to NATS request/reply. In the real design these REST
// APIs "give the same reply for all users", so for the POC a single responder on
// the intranet cluster answers the bootstrap RPC regardless of the caller.
//
// It connects to the INTRANET NATS cluster (nats-core) and answers the bootstrap
// request subject with a JSON reply shaped like the old REST /api/echo response,
// so the client's HTTP check (REST via nginx -> mobile-gateway -> NATS -> here)
// sees an equivalent region-tagged JSON body. It uses a queue group so it can be
// answered locally on whichever site the leaf lands on.
//
// Env:
//   NATS_URL         intranet core NATS URL     (default nats://nats-core-a:4222)
//   REGION           label echoed in the reply  (default unknown)
//   REQUEST_SUBJECT  bootstrap RPC subject      (default demo.bootstrap.request)
//   QUEUE_GROUP      queue group                (default bootstrap-responders)
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
)

func main() {
	natsURL := envOr("NATS_URL", "nats://nats-core-a:4222")
	region := envOr("REGION", "unknown")
	reqSubject := envOr("REQUEST_SUBJECT", "demo.bootstrap.request")
	queue := envOr("QUEUE_GROUP", "bootstrap-responders")

	nc, err := nats.Connect(natsURL,
		nats.Name("rest-nats-"+region),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		log.Fatalf("rest-nats(%s): connect to %q failed: %v", region, natsURL, err)
	}
	defer nc.Drain() //nolint:errcheck

	sub, err := nc.QueueSubscribe(reqSubject, queue, func(msg *nats.Msg) {
		log.Printf("rest-nats(%s): bootstrap request on %q (%d bytes)", region, msg.Subject, len(msg.Data))
		// Reply shape mirrors the old REST /api/echo so the client check is equivalent.
		reply := map[string]any{
			"service":   "rest-service",
			"region":    region,
			"path":      "/api/echo",
			"via":       "nats-bootstrap",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		data, _ := json.Marshal(reply)
		if err := msg.Respond(data); err != nil {
			log.Printf("rest-nats(%s): respond failed: %v", region, err)
		}
	})
	if err != nil {
		log.Fatalf("rest-nats(%s): subscribe %q failed: %v", region, reqSubject, err)
	}
	log.Printf("rest-nats(%s): connected to %s; answering %q (queue %q)", region, natsURL, reqSubject, queue)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = sub.Drain()
	log.Printf("rest-nats(%s): shutting down", region)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
