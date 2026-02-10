"""Prometheus registry wrapper."""

from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    REGISTRY,
)

# Use default registry
Registry = REGISTRY

# Pre-registered HTTP metrics
http_requests_total = Counter(
    "http_requests_total",
    "Total number of HTTP requests",
    ["service", "method", "path", "status"],
    registry=Registry,
)

http_request_duration_seconds = Histogram(
    "http_request_duration_seconds",
    "HTTP request duration in seconds",
    ["service", "method", "path"],
    registry=Registry,
)

http_requests_in_flight = Gauge(
    "http_requests_in_flight",
    "Current number of in-flight HTTP requests",
    ["service"],
    registry=Registry,
)

# Kafka producer metrics
kafka_producer_messages_total = Counter(
    "kafka_producer_messages_total",
    "Total Kafka messages sent",
    ["service", "topic", "status"],
    registry=Registry,
)

kafka_producer_buffer_usage = Gauge(
    "kafka_producer_buffer_usage",
    "Kafka producer buffer usage (0-1)",
    ["service"],
    registry=Registry,
)
