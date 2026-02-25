"""Centralized observability singleton instances.

This module provides singleton instances of logger, tracer, and metrics
to ensure consistent observability across the application.
"""
from typing import Optional
import structlog
from opentelemetry.trace import Tracer
from observability import new_logger, new_tracer, start_metrics_pusher, Status, StatusCode, get_trace_id

# Singleton instances
_logger: Optional[structlog.BoundLogger] = None
_tracer: Optional[Tracer] = None
_metrics_initialized: bool = False


def initialize_observability(service_name: str) -> None:
    """
    Initialize all observability components (logger, tracer, metrics).

    This should be called once during application startup.

    Args:
        service_name: Name of the service for observability identification
    """
    global _logger, _tracer, _metrics_initialized

    # Initialize logger
    _logger = new_logger(service_name)

    # Initialize tracer
    _tracer = new_tracer(service_name)

    # Initialize metrics pusher
    start_metrics_pusher(service_name)
    _metrics_initialized = True


def get_logger() -> structlog.BoundLogger:
    """
    Get the singleton logger instance.

    Returns:
        The configured structlog logger

    Raises:
        RuntimeError: If observability has not been initialized
    """
    if _logger is None:
        raise RuntimeError(
            "Observability not initialized. Call initialize_observability() first."
        )
    return _logger


def get_tracer() -> Tracer:
    """
    Get the singleton tracer instance.

    Returns:
        The configured OpenTelemetry tracer

    Raises:
        RuntimeError: If observability has not been initialized
    """
    if _tracer is None:
        raise RuntimeError(
            "Observability not initialized. Call initialize_observability() first."
        )
    return _tracer


def is_metrics_initialized() -> bool:
    """
    Check if metrics have been initialized.

    Returns:
        True if metrics are initialized, False otherwise
    """
    return _metrics_initialized


# Re-export Status, StatusCode, get_trace_id for convenience
__all__ = [
    "initialize_observability",
    "get_logger",
    "get_tracer",
    "is_metrics_initialized",
    "Status",
    "StatusCode",
    "get_trace_id",
]
