# Shield — UPI Fraud Detection Microservice

Shield is a fraud detection service built in Go for a PhonePe-style UPI payment system. It sits behind Kong Gateway and scores every payment/onboarding request — returning `ALLOW`, `BLOCK`, or `REVIEW` — before Kong routes to an upstream service.

---

## Architecture

```
curl / Postman
      │
      ▼
Kong OSS (Docker, port 8000)
      │
      ├── Plugin: shield-check (Lua)
      │       calls Shield at localhost:8082/check
      │       BLOCK  → 403 to caller
      │       REVIEW → 202 to caller
      │       ALLOW  → Kong routes to mock upstream
      │
      ├── /upi/pay      → mock-transaction (port 9001)
      └── /upi/register → mock-onboarding  (port 9002)

Docker Compose:
  ├── postgres   (port 5432)
  └── redis      (port 6379)

Go binaries (run directly on host):
  ├── shield            (port 8082)
  ├── mock-transaction  (port 9001)
  └── mock-onboarding   (port 9002)
```

---

## Fraud Rules

Rules run in order. Blacklist is a hard gate — if it fires, remaining rules are skipped.

| Rule | Signal | Score | Hot-path source |
|------|--------|-------|-----------------|
| Blacklist | VPA / IP / Device in blocklist | 100 → instant BLOCK | In-memory cache (refreshed from Postgres every 30s) |
| Velocity | > 5 requests per user in 60s | +40 | Redis INCR |
| Device | Unknown device for this user | +30 | `sync.Map` — Postgres INSERT fires async on first seen |
| Amount | Transaction > ₹50,000 | +20 | Pure computation |

Velocity and device rules run **concurrently** (goroutines) so latency = `max(Redis, cache lookup)`, not their sum.

**Decision thresholds (demo values):**
- Score ≥ 40 → `BLOCK`
- Score ≥ 30 → `REVIEW`
- Score < 30 → `ALLOW`

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.22+ | https://go.dev/dl/ |
| Docker Desktop | 24+ | https://www.docker.com/products/docker-desktop/ |
| Docker Compose | v2 (bundled with Docker Desktop) | — |

> **Kong OSS via Homebrew is not available.** The `kong/kong` Homebrew tap was permanently disabled in 2023. Kong is run via Docker in this setup — no separate installation needed.

---

## Local Setup

### 1. Clone the repos

```bash
git clone https://github.com/krishnaMohan2501/shield.git
git clone https://github.com/krishnaMohan2501/mock-transaction.git
git clone https://github.com/krishnaMohan2501/mock-onboarding.git
git clone https://github.com/krishnaMohan2501/kong-shield.git
```

### 2. Start Postgres and Redis

```bash
cd shield
docker compose up -d
```

Expected:
```
✔ Container shield-postgres-1  Started
✔ Container shield-redis-1     Started
```

Verify Postgres is ready:
```bash
docker exec shield-postgres-1 psql -U shield -d shield_db -c "SELECT 1;"
```

### 3. Start Shield

```bash
cd shield
go run .
```

Expected output:
```
[SHIELD] Postgres schema ready
[SHIELD] Redis connected
[SHIELD] blacklist cache refreshed: N entries   ← in-memory cache warm before first request
[SHIELD] Starting on :8082
```

Verify:
```bash
curl http://localhost:8082/health
# {"status":"ok"}
```

### 4. Start mock upstream services (two new terminals)

**Terminal 2 — mock-transaction:**
```bash
cd mock-transaction
go run .
# [MOCK-TXN] Starting on :9001
```

**Terminal 3 — mock-onboarding:**
```bash
cd mock-onboarding
go run .
# [MOCK-ONB] Starting on :9002
```

### 5. Start Kong OSS (Docker)

```bash
docker run -d --name kong \
  -p 8000:8000 -p 8001:8001 \
  --add-host=host.docker.internal:192.168.64.1 \
  -e KONG_DATABASE=off \
  -e KONG_DECLARATIVE_CONFIG=/etc/kong/kong.yml \
  -e KONG_PLUGINS=bundled,shield-check \
  -v $(pwd)/../kong-shield/kong.yml:/etc/kong/kong.yml \
  -v $(pwd)/../kong-shield/plugins/shield-check:/usr/local/share/lua/5.1/kong/plugins/shield-check \
  kong:3.9
```

> **Note on `192.168.64.1`:** This is the Mac's Docker Desktop bridge interface (`bridge100`). It's the IP Docker containers use to reach services running on your Mac. If you see `{"error":"Service temporarily unavailable"}`, find your bridge IP with `ifconfig | grep "192.168.6"` and replace `192.168.64.1` with your value.

Wait ~8 seconds, then verify Kong is up:
```bash
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-user-id: test" -H "x-device-id: d1" -H "x-amount: 100" -H "x-receiver-vpa: ok@upi"
# Returns JSON (202 REVIEW or 200 ALLOW) — any JSON response means Kong is routing correctly
# A 404 means you used GET instead of POST (routes are POST-only)
# A "000" / connection refused means Kong is not running
```

> **Why `--add-host=host.docker.internal:host-gateway`?**
> Kong runs inside Docker and needs to reach Shield and mock services running on your Mac. This flag maps `host.docker.internal` to the host machine's gateway IP — the standard way to reach host services from Docker containers on macOS.

---

## Testing

All requests go through Kong on port `8000`. Fraud signals are passed as HTTP headers.

### ALLOW — known device, normal amount

```bash
# First request — new device triggers REVIEW (score 30)
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-001" \
  -H "x-user-id: user_123" \
  -H "x-device-id: device_abc" \
  -H "x-amount: 500" \
  -H "x-receiver-vpa: merchant@okaxis"
# {"message":"Transaction flagged for review"}  ← REVIEW (202)

# Second request — device now registered, ALLOW
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-002" \
  -H "x-user-id: user_123" \
  -H "x-device-id: device_abc" \
  -H "x-amount: 500" \
  -H "x-receiver-vpa: merchant@okaxis"
# {"status":"payment_accepted","transactionId":"TXN-MOCK-001"}  ← ALLOW (200)
```

### BLOCK — blacklisted VPA

```bash
# Add a VPA to the blacklist
docker exec shield-postgres-1 psql -U shield -d shield_db \
  -c "INSERT INTO blacklist (type, value, reason) VALUES ('VPA', 'fraud@scam', 'known fraudster');"

curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: REQ-BL" \
  -H "x-user-id: user_999" \
  -H "x-device-id: device_999" \
  -H "x-amount: 100" \
  -H "x-receiver-vpa: fraud@scam"
# {"error":"Transaction blocked","reason":"blacklisted_entity"}  ← BLOCK (403)
```

### BLOCK — velocity (> 5 requests/minute)

```bash
# Step 1: register device with first request (expect REVIEW — new device)
curl -s -X POST http://localhost:8000/upi/pay \
  -H "x-request-id: WARM" \
  -H "x-user-id: spammer" \
  -H "x-device-id: d1" \
  -H "x-amount: 100" \
  -H "x-receiver-vpa: ok@upi"

# Step 2: wait for velocity window to reset, then flood (6 rapid requests)
sleep 61
for i in {1..6}; do
  curl -s -o /dev/null -w "Request $i: %{http_code}\n" \
    -X POST http://localhost:8000/upi/pay \
    -H "x-request-id: V$i" \
    -H "x-user-id: spammer" \
    -H "x-device-id: d1" \
    -H "x-amount: 100" \
    -H "x-receiver-vpa: ok@upi"
done
# Request 1: 200
# Request 2: 200
# Request 3: 200
# Request 4: 200
# Request 5: 200
# Request 6: 403  ← velocity BLOCK
```

### Verify audit log

```bash
docker exec shield-postgres-1 psql -U shield -d shield_db \
  -c "SELECT request_id, decision, risk_score, triggered_rules FROM fraud_audit_log ORDER BY created_at DESC LIMIT 10;"
```

---

## Configuration

All thresholds are in `shield/.env` — no recompile needed to change them.

```bash
VELOCITY_MAX_TXN_PER_MINUTE=5      # requests per user per 60s before velocity fires
AMOUNT_HIGH_VALUE_THRESHOLD=50000  # amount in INR above which high_amount fires
RISK_BLOCK_THRESHOLD=40            # total score >= this → BLOCK
RISK_REVIEW_THRESHOLD=30           # total score >= this → REVIEW
```

---

## Performance

### Load test results

Measured against `POST /check` directly (no Kong) with 25 concurrent VUs across three scenarios — ALLOW (known device), REVIEW (new device every iteration), BLOCK (velocity flood at 8 req/s).

| Metric | Result |
|--------|--------|
| p50 (median) | 1.85ms |
| p90 | 3.81ms |
| p95 | 5.07ms |
| **p99** | **9.51ms** |
| avg | 2.29ms |
| RPS | 425 req/s |
| Error rate | 0.00% |

Per-scenario p99:

| Scenario | p99 |
|----------|-----|
| ALLOW (known device, Redis only) | 9ms |
| BLOCK (velocity exceeded, Redis only) | 7ms |
| REVIEW (new device, async Postgres INSERT) | 14ms |

The REVIEW p99 is slightly higher because every request in that scenario registers a brand-new device, which still fires an async Postgres INSERT. In real traffic, devices repeat across sessions so the `sync.Map` cache hit rate is ~100% after warm-up.

### How to run the load test

Requires [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/) (`brew install k6`):

```bash
cd shield
k6 run load-test.js
```

The report prints at the end. Raw JSON metrics are saved to `load-test-results.json`.

### What keeps latency low

| Technique | What it does |
|-----------|-------------|
| In-memory blacklist cache | Replaces a Postgres `SELECT` on every request with a RWLock + map lookup (~0.01ms). Cache refreshes from DB every 30s in a background goroutine. |
| `sync.Map` device cache | `LoadOrStore` atomically marks a device as seen in memory. Subsequent requests for the same device skip Postgres entirely. |
| Async device INSERT | On a genuine first-seen device, the score (+30) is returned immediately and the Postgres `INSERT` fires in a goroutine — zero added latency to the response. |
| Parallel rule execution | Velocity (Redis) and device (sync.Map) run as concurrent goroutines. Latency = `max(Redis, cache lookup)` instead of their sum. |
| Async audit log | `fraud_audit_log` INSERT runs in a goroutine after the response is written — Postgres write never touches the hot path. |
| Postgres connection pool | `MaxOpenConns = MaxIdleConns = 25` keeps connections pre-established so async goroutines never wait for a new TCP handshake. |

---

## How PhonePe and Stripe achieve ultra-low latency

Both solve the same core problem: get DB calls off the synchronous request path. Shield's design is a simplified version of the same pattern.

**PhonePe / UPI systems**

- **Pre-computed risk state**: instead of querying at decision time, a background Kafka consumer continuously updates a risk profile per user in Redis. The `/check` call reads one Redis key — no Postgres in the hot path.
- **Bloom filters for blacklists**: a Bloom filter fits millions of VPAs/IPs in a few MB of RAM with zero DB lookups. False positives get a secondary Postgres check; false negatives are impossible. Shield's in-memory map is the same idea without probabilistic trade-offs.
- **Co-located caches**: Redis instances sit in the same data centre rack as the API servers. Network RTT to Redis is ~0.1ms vs. ~1ms over a local Docker bridge.
- **Decision caching with short TTL**: for the same user + device + amount bracket, the last decision is cached for 100–500ms. Repeated payment taps reuse it.

**Stripe**

- **Tiered rule evaluation**: the synchronous `/charge` call runs only the fastest rules (velocity, card BIN lookup — all Redis). Heavier ML model scoring runs on a separate async path. If the async path later flags the charge, they reverse it or queue a manual review.
- **Feature stores**: pre-computed features (30-day spend, average transaction size, device trust score) are written by a streaming pipeline and read as a single key at decision time. The model never touches raw transaction history in the hot path.
- **Tiered storage**: hot features (last 24h) in Redis, warm features (last 30 days) in ScyllaDB or DynamoDB, cold history in S3/Redshift. Scoring only touches the hot tier.
- **gRPC + persistent connection pools**: persistent gRPC connections between services eliminate TCP handshake overhead on every call.

**The shared pattern**

```
Write path (async, tolerates latency):
  Transaction event → Kafka → feature pipeline → Redis / KV store

Read path (synchronous, must be fast):
  /check → Redis + memory only → decision in < 5ms
```

Shield's architecture after optimisation follows the same model — blacklist in memory, velocity in Redis, device in `sync.Map`. The gap between Shield's 9.5ms p99 and production's ~1–2ms p99 is infrastructure, not design: same-host Redis (not Docker bridge), kernel-bypass networking, and deeper connection pools.

**Further reading:**

- [AI in Payment Processing — Redis](https://redis.io/blog/ai-in-payment-processing/)
- [How We Built It: Stripe Radar — Stripe Engineering](https://stripe.dev/blog/how-we-built-it-stripe-radar)
- [Real-Time Fraud Detection — Aerospike](https://aerospike.com/blog/real-time-fraud-detection/)

---

## Stopping Everything

```bash
# Stop Kong
docker rm -f kong

# Stop Postgres + Redis
cd shield && docker compose down

# Stop Go processes
pkill -f "go run ."
```

---

## Related Repos

| Repo | Description |
|------|-------------|
| [shield](https://github.com/krishnaMohan2501/shield) | This service — fraud scoring engine |
| [mock-transaction](https://github.com/krishnaMohan2501/mock-transaction) | Mock UPI payment upstream (port 9001) |
| [mock-onboarding](https://github.com/krishnaMohan2501/mock-onboarding) | Mock UPI registration upstream (port 9002) |
| [kong-shield](https://github.com/krishnaMohan2501/kong-shield) | Kong declarative config + shield-check Lua plugin |
