# Architecture V2 — INTRANET side (runs on the company intranet, NOT in GKE)

In the real V2 deployment the intranet side runs where the backend lives (on-prem /
a VM), and **dials OUT** to the GKE `nats-gcp` leaf LoadBalancer (firewall allows
intranet→internet only). Each region's intranet site runs:

- `nats-core-<region>` — the intranet NATS cluster. Holds BOTH:
  - `leafnodes { remotes: [ { url: <GKE nats-gcp-leaf LB>, hub: true } ] }`  ← dials the
    GKE hub OUTBOUND, with **hub:true** so leaf-learned interest propagates to the
    gateway (REQUIRED — without it cross-region req/reply + events fail; see
    PROJECT_PLAN §7g.4).
  - `gateway { ... }` — supercluster peer = the OTHER region's intranet core
    (intranet↔intranet; this does NOT traverse GKE).
- `rest-<region>` — the region's REST API (dummy here; real REST in production).
- `worker-<region>` — req/reply responder + room-event publisher (dummy).

For the POC this is modeled with docker-compose on a VM (see
`docker-compose.intranet-region-a.yml` / `-region-b.yml`). In production these are the
real backend cluster + services; only the leaf `remotes` URL (the GKE leaf LB) and the
gateway peer URL are new wiring.

## Addresses to fill in (env or compose)
- `GCP_HUB_URL`  = `nats-leaf://<region GKE nats-gcp-leaf LB host>:7422`  (what to dial OUT)
- `PEER_GW_URL`  = `nats://<other region intranet gateway host>:7222`     (supercluster peer)

## Why two files
region-a's intranet dials ONLY the region-a GKE hub; region-b's dials ONLY region-b's.
The gateway joins the two intranet cores. Run each on its own site/VM (or both on one VM
with two isolated networks for a local test, exactly as `poc-v2/docker-compose.yml` does).

## Local equivalent already proven
`poc-v2/docker-compose.yml` (two isolated nets) already runs this whole thing — GKE side
+ intranet side together — on one box. These intranet-only compose files are the split-out
intranet half for the real deployment where the GKE half lives in GKE instead.
