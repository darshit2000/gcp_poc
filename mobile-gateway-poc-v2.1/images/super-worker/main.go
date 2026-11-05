// worker simulates a backend that lives on ONE region's NATS cluster. One image,
// run once per region (REGION env). It does the two things a real backend does
// that a cross-region client must be able to reach via the supercluster:
//
//  1. Request/reply: answers requests on this region's request subject.
//  2. Live events: periodically publishes room events on this region's "global"
//     room-event subject (gateway-propagated, like chat.room.{id}.event).
//
// Running a worker on BOTH regions lets us prove bidirectional federation: a
// client attached to either region receives BOTH its local-region events AND the
// other region's events (forwarded by the supercluster), and request/reply works
// to either region.
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
	natsURL := envOr("NATS_URL", nats.DefaultURL)
	region := envOr("REGION", "unknown")
	// Per-region subjects. The request subject is region-specific (a responder
	// for that region); the event subject is a "global" room subject the
	// supercluster propagates to interested clients in any region.
	reqSubject := envOr("REQUEST_SUBJECT", "demo."+region+".request")
	eventSubject := envOr("EVENT_SUBJECT", "demo.room."+region+".event")
	queue := envOr("QUEUE_GROUP", region+"-workers")

	nc, err := nats.Connect(natsURL,
		nats.Name("worker-"+region),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		log.Fatalf("worker(%s): connect to %q failed: %v", region, natsURL, err)
	}
	defer nc.Drain() //nolint:errcheck

	// 1) Request/reply responder for this region.
	sub, err := nc.QueueSubscribe(reqSubject, queue, func(msg *nats.Msg) {
		log.Printf("worker(%s): request on %q (%d bytes)", region, msg.Subject, len(msg.Data))
		reply := map[string]any{
			"service":   "worker",
			"region":    region,
			"subject":   msg.Subject,
			"echo":      string(msg.Data),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		data, _ := json.Marshal(reply)
		if err := msg.Respond(data); err != nil {
			log.Printf("worker(%s): respond failed: %v", region, err)
		}
	})
	if err != nil {
		log.Fatalf("worker(%s): subscribe %q failed: %v", region, reqSubject, err)
	}
	log.Printf("worker(%s): connected to %s; reply=%q; events=%q", region, natsURL, reqSubject, eventSubject)

	// 2) Periodic room-event publisher (simulates live room activity in this region).
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		n := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				n++
				evt := map[string]any{
					"service":   "worker",
					"region":    region,
					"type":      "room_event",
					"seq":       n,
					"subject":   eventSubject,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				}
				data, _ := json.Marshal(evt)
				if err := nc.Publish(eventSubject, data); err != nil {
					log.Printf("worker(%s): publish event failed: %v", region, err)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(stop)
	_ = sub.Drain()
	log.Printf("worker(%s): shutting down", region)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
