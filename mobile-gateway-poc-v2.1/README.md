# Mobile Gateway POC (V2.1) — deployed configs + image code

Generic names only (region-a / region-b / region-c). This folder is exactly what
is deployed: a GKE cluster (cloud side) + one intranet VM (intranet side), joined by
a single NATS leaf link that is DIALED OUTBOUND from the intranet through an egress
reverse proxy ("adapter"). Addresses are env placeholders, substituted at deploy time.

## Directories
- `gke-single-cluster/` — runs in GKE (one cluster, namespaces region-a + region-b).
  Per region: nats-gcp (client WebSocket + leaf-hub listener :7422), a TCP LoadBalancer
  for the leaf (`nats-gcp-leaf`),  the mobile-gateway (HTTP->NATS bridge), nginx, and a
  public `proxy` LoadBalancer. `00-namespaces.yaml`, `10-region-a.yaml`, `11-region-b.yaml`.
  Placeholders:`__AR_IMAGE_PREFIX__` (Artifact Registry), `__INTRANET_EGRESS_CIDR__`
  (lock the leaf LB to the intranet egress).

- `intranet/` — runs on the intranet VM via docker-compose (3 isolated networks).
  `docker-compose.intranet-region-a.yml` = nats-core-a (the SINGLE leaf egress) + rest-nats + worker.
  `docker-compose.intranet-region-b.yml` = nats-core-b (pure supercluster peer, NO leaf) + rest-nats + worker.
  `docker-compose.intranet-adapter.yml`  = the egress reverse proxy (region-c), an L4 TCP relay.

- `nats/` — the four NATS server configs mounted by the intranet composes / baked into GKE.
  `nats-gcp-a|b.conf` = cloud side (leaf HUB listener). `nats-core-a|b.conf` = intranet side
  (leaf dialer + gateway/supercluster).

- `images/` — source for the four container images used in the run:
  `mobile-gateway/` (HTTP->NATS bridge), `rest-nats/` (ex-REST service as a NATS responder),
  `super-worker/` (request/reply + events), `super-client/` (the test client).

## Data path
Client -> GKE `proxy` LB -> nginx: `/nats` (WebSocket) -> nats-gcp; `/api/*` (HTTP) ->
mobile-gateway, which turns each HTTP call into ONE NATS request/reply. The only
internet<->intranet link is the NATS leaf, opened from the intranet (firewall-friendly).
The two intranet cores form a supercluster so either region can reach the other.

See `nats/nats-core-a.conf`, the `leafnodes { remotes: [ { url: ..., hub: true } ] }` block.
Because the intranet DIALS the leaf it is a NATS "spoke" by default; a spoke does not
re-advertise leaf-learned interest across its gateway, so `hub: true` is required to make
the dialing side act as the NATS hub (interest propagates to cluster + gateway) while still
being the side that opens the TCP connection. Cloud side shows `is_spoke: true` in /leafz;
intranet side `is_spoke: false`.