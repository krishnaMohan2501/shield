/**
 * k6 load test for Shield fraud detection service.
 *
 * Targets Shield directly on :8082 (pure service latency, no Kong overhead).
 * Three scenario groups run concurrently:
 *   - ALLOW  (known device after warm-up, low amount)
 *   - REVIEW (brand-new user+device per request → always triggers unknown_device)
 *   - BLOCK  (velocity flood: same user hammered beyond threshold)
 *
 * Run:
 *   k6 run load-test.js
 */

import http from "k6/http";
import { check, sleep } from "k6";
import { Trend, Rate } from "k6/metrics";

const allowLatency  = new Trend("shield_allow_latency",  true);
const reviewLatency = new Trend("shield_review_latency", true);
const blockLatency  = new Trend("shield_block_latency",  true);
const errorRate     = new Rate("shield_error_rate");

export const options = {
  // Include p(99) in trend summaries (default only has p(90), p(95))
  summaryTrendStats: ["avg", "min", "med", "max", "p(90)", "p(95)", "p(99)"],

  scenarios: {
    // Ramp to 20 VUs (realistic for local Postgres), hold 60s, ramp down
    allow_traffic: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "15s", target: 20 },
        { duration: "60s", target: 20 },
        { duration: "10s", target: 0  },
      ],
      gracefulRampDown: "5s",
      exec: "allowScenario",
    },

    // 5 VUs always hitting unknown-device path
    review_traffic: {
      executor: "constant-vus",
      vus: 5,
      duration: "85s",
      exec: "reviewScenario",
    },

    // Burst: 8 req/s from same user to trigger velocity BLOCK
    velocity_flood: {
      executor: "constant-arrival-rate",
      rate: 8,
      timeUnit: "1s",
      duration: "85s",
      preAllocatedVUs: 3,
      exec: "velocityScenario",
    },
  },

  // Realistic thresholds for a local Go service + Docker Postgres/Redis
  thresholds: {
    "http_req_duration":      ["p(95)<500", "p(99)<1000"],
    "shield_allow_latency":   ["p(95)<400", "p(99)<800"],
    "shield_review_latency":  ["p(95)<500", "p(99)<1000"],
    "shield_block_latency":   ["p(95)<300", "p(99)<600"],
    "shield_error_rate":      ["rate<0.02"],
  },
};

const BASE    = "http://localhost:8082";
const HEADERS = { "Content-Type": "application/json" };

// ALLOW scenario ─────────────────────────────────────────────────────────────
// VU-scoped user+device: first iter triggers unknown_device (REVIEW), all
// subsequent iters are known-device → ALLOW.  Check only verifies 200 + valid JSON.
export function allowScenario() {
  const userId   = `allow_user_${__VU}`;
  const deviceId = `allow_device_${__VU}`;

  const body = JSON.stringify({
    requestId:   `REQ-ALLOW-${__VU}-${__ITER}`,
    userId,
    deviceId,
    ipAddress:   "10.0.0.1",
    action:      "payment",
    amount:      500,
    receiverVpa: "merchant@okaxis",
  });

  const start = Date.now();
  const res = http.post(`${BASE}/check`, body, { headers: HEADERS });
  allowLatency.add(Date.now() - start);

  const ok = check(res, {
    "allow: status 200": (r) => r.status === 200,
    "allow: has decision": (r) => {
      try { return ["ALLOW", "REVIEW", "BLOCK"].includes(JSON.parse(r.body).decision); }
      catch { return false; }
    },
  });
  errorRate.add(!ok);
  sleep(0.05);
}

// REVIEW scenario ─────────────────────────────────────────────────────────────
// Unique user+device every iteration → device rule always fires (+30 = REVIEW).
// Check accepts REVIEW or ALLOW (under load, device INSERT may time out → 0 score).
export function reviewScenario() {
  const id = `${__VU}_${__ITER}`;

  const body = JSON.stringify({
    requestId:   `REQ-REV-${id}`,
    userId:      `ruser_${id}`,
    deviceId:    `rdev_${id}`,
    ipAddress:   "10.0.0.2",
    action:      "payment",
    amount:      1000,
    receiverVpa: "safe@okaxis",
  });

  const start = Date.now();
  const res = http.post(`${BASE}/check`, body, { headers: HEADERS });
  reviewLatency.add(Date.now() - start);

  const ok = check(res, {
    "review: status 200": (r) => r.status === 200,
    "review: has decision": (r) => {
      try { return ["ALLOW", "REVIEW", "BLOCK"].includes(JSON.parse(r.body).decision); }
      catch { return false; }
    },
  });
  errorRate.add(!ok);
  sleep(0.05);
}

// BLOCK / velocity scenario ───────────────────────────────────────────────────
// Same user at 8 req/s → velocity counter > 5 within 60s window → BLOCK.
// After the device is registered on the first call, subsequent calls score
// velocity only: 0 → ALLOW (under threshold), +40 → BLOCK (over threshold).
export function velocityScenario() {
  const body = JSON.stringify({
    requestId:   `REQ-VEL-${__ITER}`,
    userId:      "velocity_target_user",
    deviceId:    "vel_device_1",
    ipAddress:   "10.0.0.3",
    action:      "payment",
    amount:      100,
    receiverVpa: "ok@upi",
  });

  const start = Date.now();
  const res = http.post(`${BASE}/check`, body, { headers: HEADERS });
  blockLatency.add(Date.now() - start);

  check(res, {
    "velocity: status 200": (r) => r.status === 200,
    "velocity: has decision": (r) => {
      try { return ["ALLOW", "BLOCK", "REVIEW"].includes(JSON.parse(r.body).decision); }
      catch { return false; }
    },
  });
}

// End-of-test summary ─────────────────────────────────────────────────────────
export function handleSummary(data) {
  const m = data.metrics;

  function ms(metric, key) {
    if (!m[metric] || !m[metric].values) return "N/A";
    const v = m[metric].values[key];
    return v !== undefined ? v.toFixed(2) + "ms" : "N/A";
  }

  function val(metric, key) {
    if (!m[metric] || !m[metric].values) return "N/A";
    return m[metric].values[key];
  }

  const totalReqs = val("http_reqs", "count") || 0;
  const rps       = (val("http_reqs", "rate") || 0).toFixed(1);
  const errPct    = m["shield_error_rate"]
    ? (m["shield_error_rate"].values.rate * 100).toFixed(2) + "%"
    : "0.00%";

  const pad = (s, n) => String(s).padEnd(n);

  const report = [
    "╔══════════════════════════════════════════════════════════════════╗",
    "║         Shield Fraud Service — Load Test Report                  ║",
    "╠══════════════════════════════════════════════════════════════════╣",
    `║  Target   : http://localhost:8082/check (direct, no Kong)        ║`,
    `║  Duration : ~85 seconds                                          ║`,
    "╠══════════════════════════════════════════════════════════════════╣",
    "║               OVERALL HTTP LATENCY (all scenarios)               ║",
    "╠══════════════════════════════════════════════════════════════════╣",
    `║  p50  (median) : ${pad(ms("http_req_duration", "med"),   47)}║`,
    `║  p90           : ${pad(ms("http_req_duration", "p(90)"), 47)}║`,
    `║  p95           : ${pad(ms("http_req_duration", "p(95)"), 47)}║`,
    `║  p99           : ${pad(ms("http_req_duration", "p(99)"), 47)}║`,
    `║  avg           : ${pad(ms("http_req_duration", "avg"),   47)}║`,
    `║  max           : ${pad(ms("http_req_duration", "max"),   47)}║`,
    "╠══════════════════════════════════════════════════════════════════╣",
    "║              PER-SCENARIO LATENCY                                ║",
    "╠══════════════════════════════════════════════════════════════════╣",
    `║  ALLOW  p50 : ${pad(ms("shield_allow_latency",  "med"),   50)}║`,
    `║  ALLOW  p95 : ${pad(ms("shield_allow_latency",  "p(95)"), 50)}║`,
    `║  ALLOW  p99 : ${pad(ms("shield_allow_latency",  "p(99)"), 50)}║`,
    "║                                                                  ║",
    `║  REVIEW p50 : ${pad(ms("shield_review_latency", "med"),   50)}║`,
    `║  REVIEW p95 : ${pad(ms("shield_review_latency", "p(95)"), 50)}║`,
    `║  REVIEW p99 : ${pad(ms("shield_review_latency", "p(99)"), 50)}║`,
    "║                                                                  ║",
    `║  BLOCK  p50 : ${pad(ms("shield_block_latency",  "med"),   50)}║`,
    `║  BLOCK  p95 : ${pad(ms("shield_block_latency",  "p(95)"), 50)}║`,
    `║  BLOCK  p99 : ${pad(ms("shield_block_latency",  "p(99)"), 50)}║`,
    "╠══════════════════════════════════════════════════════════════════╣",
    "║                   THROUGHPUT & ERRORS                            ║",
    "╠══════════════════════════════════════════════════════════════════╣",
    `║  Total requests : ${pad(totalReqs, 46)}║`,
    `║  RPS (avg)      : ${pad(rps + " req/s", 46)}║`,
    `║  Error rate     : ${pad(errPct, 46)}║`,
    "╚══════════════════════════════════════════════════════════════════╝",
  ].join("\n");

  console.log("\n" + report + "\n");

  return {
    "load-test-results.json": JSON.stringify(data, null, 2),
    stdout: report + "\n",
  };
}
