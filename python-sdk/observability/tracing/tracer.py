"""Tracer factory for Python services."""

import os
import sys
from typing import Optional

from opentelemetry import trace
from opentelemetry.sdk.resources import Resource, SERVICE_NAME, SERVICE_VERSION
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import ParentBasedTraceIdRatio

from observability.tracing.kafka_exporter import KafkaSpanExporter


_tracer_provider: Optional[TracerProvider] = None


def new_tracer(service_name: str) -> trace.Tracer:
    """Create a new OpenTelemetry tracer with Kafka exporter.
    
    Args:
        service_name: The name of the service for trace identification.
    
    Returns:
        A configured OpenTelemetry Tracer.
    """
    global _tracer_provider
    
    # Get configuration from environment
    environment = os.getenv("ENVIRONMENT", "production")
    service_version = os.getenv("SERVICE_VERSION", "unknown")
    
    # Parse sampling rate
    try:
        sampling_rate = float(os.getenv("TRACE_SAMPLING_RATE", "0.1"))
    except ValueError:
        sampling_rate = 0.1
    
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
    
    # Add Kafka exporter if not in development mode and enabled
    enable_kafka = os.getenv("TRACES_KAFKA_ENABLED", "true").lower() in ("true", "1", "yes")
    
    if environment != "development" and enable_kafka:
        brokers = os.getenv("KAFKA_BROKERS", "")
        if brokers:
            kafka_brokers = [b.strip() for b in brokers.split(",")]
            topic = os.getenv("KAFKA_TRACES_TOPIC", "traces.application")
            
            exporter = KafkaSpanExporter(service_name, kafka_brokers, topic)
            _tracer_provider.add_span_processor(BatchSpanProcessor(exporter))
        else:
            sys.stderr.write("Warning: KAFKA_BROKERS not set, trace export disabled\n")
    
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
