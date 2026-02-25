"""Tracer factory for Python services."""

import os
import sys
from typing import Optional

from opentelemetry import trace
from opentelemetry.sdk.resources import Resource, SERVICE_NAME, SERVICE_VERSION
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import ParentBasedTraceIdRatio
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter


_tracer_provider: Optional[TracerProvider] = None


def new_tracer(service_name: str) -> trace.Tracer:
    """Create a new OpenTelemetry tracer with OTLP exporter.
    
    Args:
        service_name: The name of the service for trace identification.
    
    Returns:
        A configured OpenTelemetry Tracer.
    """
    global _tracer_provider
    
    # Get configuration from environment
    environment = _get_env("ENVIRONMENT", "production")
    service_version = os.getenv("SERVICE_VERSION", "unknown")
    
    # Parse sampling rate (default to 1.0 if not set or invalid)
    try:
        sampling_rate = float(os.getenv("TRACE_SAMPLING_RATE", "1.0"))
    except ValueError:
        sampling_rate = 1.0
    
    # Create resource
    resource = Resource.create({
        SERVICE_NAME: service_name,
        SERVICE_VERSION: service_version,
        "deployment.environment": environment,
    })
    
    # Create tracer provider with sampling
    _tracer_provider = TracerProvider(
        resource=resource,
        sampler=ParentBasedTraceIdRatio(sampling_rate),
    )
    
    # Configure OTLP span exporter (sends to OTEL collector agent).
    # Endpoint must be a full URL (e.g. http://host:4317); do not strip scheme.
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
    exporter = OTLPSpanExporter(endpoint=endpoint)
    _tracer_provider.add_span_processor(BatchSpanProcessor(exporter))
    
    # Set as global provider
    trace.set_tracer_provider(_tracer_provider)
    
    return trace.get_tracer(service_name)


def get_trace_id() -> str:
    """Get the current trace ID from the active span.
    
    Returns:
        The trace ID as a hex string, or empty string if no active span.
    """
    span = trace.get_current_span()
    ctx = span.get_span_context()
    if ctx.is_valid:
        return format(ctx.trace_id, "032x")
    return ""


def shutdown_tracer():
    """Shutdown the tracer provider."""
    global _tracer_provider
    if _tracer_provider:
        _tracer_provider.shutdown()
        _tracer_provider = None


def _get_env(key: str, default: str) -> str:
    """Get environment variable with fallback names for ENVIRONMENT."""
    if key == "ENVIRONMENT":
        for k in ["ENVIRONMENT", "ENV", "environment", "env"]:
            v = os.getenv(k)
            if v:
                return v
    
    return os.getenv(key, default)
