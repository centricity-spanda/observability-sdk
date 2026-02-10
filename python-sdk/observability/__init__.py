"""
Platform Observability SDK for Python

Provides unified telemetry collection (logs, metrics, traces) with Kafka integration.
"""

from observability.logging import new_logger, LogConfig
from observability.metrics import start_metrics_pusher, stop_metrics_pusher, Registry
from observability.tracing import new_tracer, get_trace_id

__all__ = [
    # Logging
    "new_logger",
    "LogConfig",
    # Metrics
    "start_metrics_pusher",
    "stop_metrics_pusher",
    "Registry",
    # Tracing
    "new_tracer",
    "get_trace_id",
]

__version__ = "0.1.0"
