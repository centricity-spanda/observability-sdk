#!/usr/bin/env python3
"""
Alert load-test script for observability demo.

Demonstrates real scenarios that trigger Grafana alerts and logs exactly
what is sent, when errors occur, and when each alert is expected to fire.

Usage:
  python alert-load-test.py baseline      # Normal traffic (5+ min) - no alerts
  python alert-load-test.py error-rate   # Mix success + /api/error -> High Error Rate
  python alert-load-test.py in-flight    # 60 concurrent requests -> High In-Flight
  python alert-load-test.py large-payload   # POST >1MB body -> Large Request Payload
  python alert-load-test.py high-latency    # Slow requests -> High P99 Latency
  python alert-load-test.py demo         # Full scenario with timeline and logging

Alert rules (from alerting.yaml):
  - High Error Rate:     >5% 5xx in 5m window, for 1m  -> then alert sent
  - High P99 Latency:    P99 > 2s over 5m, for 2m      -> then alert sent
  - Large Request:       P99 request size > 1MB, for 30s -> then alert sent
  - High In-Flight:      in-flight > 50, for 1m       -> then alert sent
  - Kafka Producer:      any producer errors, for 1m    -> then alert sent
"""

import logging
import sys
import time
import urllib.request
import urllib.error
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime

# -----------------------------------------------------------------------------
# Configuration (change BASE / port if your service runs elsewhere)
# -----------------------------------------------------------------------------
BASE = "http://localhost:8081"
LARGE_PAYLOAD_BYTES = 1_500_000  # 1.5 MB (> 1MB threshold)
ALERT_FOR_DURATIONS = {
    "High Error Rate": "1m",
    "High P99 Latency": "2m",
    "Large Request Payload": "30s",
    "High In-Flight Requests": "1m",
    "Kafka Producer Failures": "1m",
}


def ts() -> str:
    return datetime.utcnow().strftime("%H:%M:%S.%f")[:-3] + "Z"


def setup_logging(verbose: bool = True) -> None:
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(
        level=level,
        format="%(asctime)s.%(msecs)03d [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )


def log_send(method: str, path: str, extra: str = "") -> None:
    logging.info("[SEND] %s %s%s", method, path, f" {extra}" if extra else "")


def log_response(status: int, path: str, extra: str = "") -> None:
    if status >= 400:
        logging.warning("[RESPONSE] %s %s -> %d%s", ts(), path, status, f" {extra}" if extra else "")
    else:
        logging.debug("[RESPONSE] %s %s -> %d%s", ts(), path, status, f" {extra}" if extra else "")


def log_error_occurred(path: str, status: int, detail: str = "") -> None:
    logging.warning("[ERROR OCCURRED] %s returned %d. %s", path, status, detail or "This counts toward High Error Rate.")


def log_alert_expected(alert_name: str, when: str) -> None:
    for_dur = ALERT_FOR_DURATIONS.get(alert_name, "?")
    logging.info("[ALERT] %s is expected to fire after condition holds for %s (rule: for %s)", alert_name, when, for_dur)


# -----------------------------------------------------------------------------
# HTTP helpers
# -----------------------------------------------------------------------------
def get(path: str = "/api/users") -> tuple[str, int]:
    try:
        req = urllib.request.Request(BASE + path)
        with urllib.request.urlopen(req, timeout=15) as r:
            return "ok", r.status
    except urllib.error.HTTPError as e:
        return f"error:{e.code}", e.code
    except Exception as e:
        return str(e), 0


def post_error(count: int = 1) -> tuple[str, int]:
    try:
        req = urllib.request.Request(
            BASE + f"/api/error?count={count}",
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as r:
            return "ok", r.status
    except urllib.error.HTTPError as e:
        return f"error:{e.code}", e.code
    except Exception as e:
        return str(e), 0


def post_large_payload(size: int) -> tuple[str, int]:
    body = b"x" * size
    try:
        req = urllib.request.Request(
            BASE + "/api/large-payload",
            data=body,
            method="POST",
            headers={"Content-Type": "application/octet-stream"},
        )
        with urllib.request.urlopen(req, timeout=30) as r:
            return "ok", r.status
    except urllib.error.HTTPError as e:
        return f"error:{e.code}", e.code
    except Exception as e:
        return str(e), 0


def post_stress(duration_ms: int = 2500) -> tuple[str, int]:
    try:
        req = urllib.request.Request(
            BASE + f"/api/stress?duration_ms={duration_ms}",
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=duration_ms // 1000 + 10) as r:
            return "ok", r.status
    except urllib.error.HTTPError as e:
        return f"error:{e.code}", e.code
    except Exception as e:
        return str(e), 0


# -----------------------------------------------------------------------------
# Scenarios
# -----------------------------------------------------------------------------
def run_baseline(minutes: int = 6, interval: float = 2.0) -> None:
    """Normal traffic so histograms are stable; no alerts expected."""
    logging.info("=== BASELINE: normal traffic for %d min (no alerts expected) ===", minutes)
    log_send("GET", "/api/users", f"every {interval}s")
    end = time.time() + minutes * 60
    count = 0
    while time.time() < end:
        _, status = get("/api/users")
        count += 1
        log_response(status, "/api/users", f"total requests={count}")
        time.sleep(interval)
    logging.info("Baseline done. Total requests: %d. No alerts expected.", count)


def run_error_rate(minutes: int = 2, error_every_n: int = 4) -> None:
    """Mix success and /api/error so error rate > 5%; triggers High Error Rate after 1m."""
    logging.info("=== ERROR RATE: 1 in %d calls to POST /api/error (target: >5%% 5xx) ===", error_every_n)
    log_alert_expected("High Error Rate", "~1 minute from now")
    end = time.time() + minutes * 60
    count = 0
    errors = 0
    while time.time() < end:
        count += 1
        if count % error_every_n == 0:
            log_send("POST", "/api/error", "intentional 500 for demo")
            _, status = post_error(count)
            errors += 1
            log_error_occurred("/api/error", status, "Intentional 500 for High Error Rate demo.")
            log_response(status, "/api/error")
        else:
            _, status = get("/api/users")
            log_response(status, "/api/users")
        time.sleep(2)
    rate = (errors / count * 100) if count else 0
    logging.info("Error rate done. Requests=%d, errors=%d (%.1f%%). Alert fires after condition for 1m.", count, errors, rate)


def run_in_flight(concurrent: int = 60, duration_sec: int = 70) -> None:
    """Many concurrent requests so in-flight > 50; triggers High In-Flight after 1m."""
    logging.info("=== IN-FLIGHT: %d concurrent GET /api/users for %ds ===", concurrent, duration_sec)
    log_alert_expected("High In-Flight Requests", "~1 minute from now")
    end = time.time() + duration_sec
    total = 0
    with ThreadPoolExecutor(max_workers=concurrent) as ex:
        while time.time() < end:
            futures = [ex.submit(get) for _ in range(concurrent)]
            for f in as_completed(futures):
                f.result()
            total += concurrent
            time.sleep(1)
    logging.info("In-flight done. ~%d requests. Alert fires after in-flight>50 for 1m.", total)


def run_large_payload(num_requests: int = 20, size: int = LARGE_PAYLOAD_BYTES, interval: float = 2.0) -> None:
    """POST body > 1MB so P99 request size > 1MB; triggers Large Request Payload after 30s."""
    logging.info("=== LARGE PAYLOAD: POST /api/large-payload with body=%s bytes (~%.1f MB) ===", f"{size:,}", size / (1024 * 1024))
    log_alert_expected("Large Request Payload", "~30 seconds from first large request")
    for i in range(num_requests):
        log_send("POST", "/api/large-payload", f"body_size={size:,} bytes (request {i+1}/{num_requests})")
        _, status = post_large_payload(size)
        log_response(status, "/api/large-payload", f"size={size:,}")
        if i < num_requests - 1:
            time.sleep(interval)
    logging.info("Large payload done. %d requests of %s bytes. Alert fires after P99>1MB for 30s.", num_requests, f"{size:,}")


def run_high_latency(num_slow: int = 10, duration_ms: int = 2500, interval: float = 5.0) -> None:
    """POST /api/stress with duration_ms so some requests take >2s; triggers High P99 Latency after 2m."""
    logging.info("=== HIGH LATENCY: POST /api/stress duration_ms=%d (P99 > 2s) ===", duration_ms)
    log_alert_expected("High P99 Latency", "~2 minutes from now")
    for i in range(num_slow):
        log_send("POST", f"/api/stress?duration_ms={duration_ms}", f"slow request {i+1}/{num_slow}")
        _, status = post_stress(duration_ms)
        log_response(status, "/api/stress", f"duration_ms={duration_ms}")
        if i < num_slow - 1:
            time.sleep(interval)
    logging.info("High latency done. %d slow requests (%.1fs each). Alert fires after P99>2s for 2m.", num_slow, duration_ms / 1000)


def run_demo() -> None:
    """
    Full demo: baseline, then trigger error-rate and large-payload with clear logging.
    Logs what is sent, when errors occur, and when each alert will be sent.
    """
    logging.info("========== ALERT DEMO – REAL SCENARIO ==========")
    logging.info("Service: %s", BASE)
    logging.info("")
    logging.info("Alert rules (condition must hold for 'for' duration before alert is sent):")
    for name, dur in ALERT_FOR_DURATIONS.items():
        logging.info("  - %s: for %s", name, dur)
    logging.info("")

    # 1) Short baseline so we have some normal traffic
    logging.info("Step 1: Short baseline (1 min normal traffic)")
    end = time.time() + 60
    n = 0
    while time.time() < end:
        _, status = get("/api/users")
        n += 1
        log_response(status, "/api/users", f"#{n}")
        time.sleep(2)
    logging.info("Baseline done (%d requests).", n)
    logging.info("")

    # 2) Error rate: send errors and log each one
    logging.info("Step 2: Trigger High Error Rate (mix /api/users and /api/error)")
    log_alert_expected("High Error Rate", "after ~1 min of >5%% errors")
    for i in range(24):
        if i % 4 == 0:
            log_send("POST", "/api/error", "intentional 500")
            _, status = post_error()
            log_error_occurred("/api/error", status, "Intentional 500 – counts toward error rate.")
            log_response(status, "/api/error")
        else:
            _, status = get("/api/users")
            log_response(status, "/api/users")
        time.sleep(5)
    logging.info("Error-rate phase done. Check Grafana: High Error Rate should fire if >5%% for 1m.")
    logging.info("")

    # 3) Large payload: log each large request
    logging.info("Step 3: Trigger Large Request Payload (POST body > 1MB)")
    log_alert_expected("Large Request Payload", "after ~30s of P99 request size > 1MB")
    for i in range(15):
        log_send("POST", "/api/large-payload", f"body={LARGE_PAYLOAD_BYTES:,} bytes")
        _, status = post_large_payload(LARGE_PAYLOAD_BYTES)
        log_response(status, "/api/large-payload", f"size={LARGE_PAYLOAD_BYTES:,}")
        time.sleep(2)
    logging.info("Large-payload phase done. Check Grafana: Large Request Payload should fire after 30s.")
    logging.info("")

    logging.info("========== DEMO COMPLETE ==========")
    logging.info("In Grafana: Alerts > Alert rules. Look for 'High Error Rate' and 'Large Request Payload'.")
    logging.info("Logs above show what was sent and when errors occurred so you can correlate with alerts.")


# -----------------------------------------------------------------------------
# Entrypoint
# -----------------------------------------------------------------------------
def main() -> int:
    setup_logging(verbose=True)
    if len(sys.argv) < 2:
        print(__doc__)
        return 0
    cmd = sys.argv[1].lower().replace("_", "-")
    if cmd == "baseline":
        run_baseline()
    elif cmd == "error-rate":
        run_error_rate()
    elif cmd == "in-flight":
        run_in_flight()
    elif cmd == "large-payload":
        run_large_payload()
    elif cmd == "high-latency":
        run_high_latency()
    elif cmd == "demo":
        run_demo()
    else:
        logging.error("Unknown command: %s. Use: baseline | error-rate | in-flight | large-payload | high-latency | demo", sys.argv[1])
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
