"""Kafka span exporter for OpenTelemetry."""

import os
import sys
import threading
from queue import Queue, Full
from typing import Optional, Sequence

from kafka import KafkaProducer
from kafka.errors import KafkaError
from opentelemetry.sdk.trace import ReadableSpan
from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)
from opentelemetry.proto.common.v1.common_pb2 import AnyValue, KeyValue
from opentelemetry.proto.trace.v1.trace_pb2 import (
    ResourceSpans,
    ScopeSpans,
    Span,
    Status,
)
from opentelemetry.proto.resource.v1.resource_pb2 import Resource


class KafkaSpanExporter(SpanExporter):
    """Exports OpenTelemetry spans to Kafka in OTLP protobuf format."""
    
    def __init__(
        self,
        service_name: str,
        kafka_brokers: list[str],
        topic: str,
    ):
        self.service_name = service_name
        self.topic = topic
        self._producer: Optional[KafkaProducer] = None
        self._queue: Queue = Queue(maxsize=1000)
        self._closed = False
        self._lock = threading.Lock()
        
        self._init_producer(kafka_brokers)
        if self._producer:
            self._start_sender_thread()
    
    def _init_producer(self, kafka_brokers: list[str]):
        """Initialize Kafka producer."""
        try:
            self._producer = KafkaProducer(
                bootstrap_servers=kafka_brokers,
                acks=1,
                compression_type="gzip",
            )
        except KafkaError as e:
            sys.stderr.write(f"Failed to initialize trace Kafka producer: {e}\n")
            self._producer = None
    
    def _start_sender_thread(self):
        """Start background sender thread."""
        thread = threading.Thread(target=self._sender_loop, daemon=True)
        thread.start()
    
    def _sender_loop(self):
        """Background loop to send spans."""
        while not self._closed:
            try:
                span_data = self._queue.get(timeout=1.0)
                if span_data and self._producer:
                    self._producer.send(
                        self.topic,
                        key=span_data["key"],
                        value=span_data["value"],
                    )
            except Exception:
                pass
    
    def export(self, spans: Sequence[ReadableSpan]) -> SpanExportResult:
        """Export spans to Kafka in OTLP protobuf format."""
        if self._closed or not self._producer:
            return SpanExportResult.FAILURE
        
        # Convert to OTLP and serialize
        otlp_request = self._convert_to_otlp(spans)
        protobuf_data = otlp_request.SerializeToString()
        
        # Use first span's trace ID as key
        key = b""
        if spans:
            ctx = spans[0].get_span_context()
            key = format(ctx.trace_id, "032x").encode()
        
        try:
            self._queue.put_nowait({
                "key": key,
                "value": protobuf_data,
            })
        except Full:
            pass  # Drop if queue full
        
        return SpanExportResult.SUCCESS
    
    def shutdown(self) -> None:
        """Shutdown the exporter."""
        with self._lock:
            self._closed = True
            if self._producer:
                self._producer.flush()
                self._producer.close()
    
    def force_flush(self, timeout_millis: int = 30000) -> bool:
        """Force flush any pending spans."""
        if self._producer:
            self._producer.flush(timeout=timeout_millis / 1000)
        return True
    
    def _convert_to_otlp(self, spans: Sequence[ReadableSpan]) -> ExportTraceServiceRequest:
        """Convert spans to OTLP format."""
        otlp_spans = []
        
        for span in spans:
            ctx = span.get_span_context()
            parent = span.parent
            
            # Convert attributes
            attributes = []
            if span.attributes:
                for key, value in span.attributes.items():
                    attributes.append(
                        KeyValue(
                            key=key,
                            value=self._value_to_otlp(value)
                        )
                    )
            
            # Convert events
            events = []
            if span.events:
                for event in span.events:
                    event_attrs = []
                    if event.attributes:
                        for key, value in event.attributes.items():
                            event_attrs.append(
                                KeyValue(
                                    key=key,
                                    value=self._value_to_otlp(value)
                                )
                            )
                    events.append(
                        Span.Event(
                            time_unix_nano=event.timestamp,
                            name=event.name,
                            attributes=event_attrs,
                        )
                    )
            
            # Convert status
            status = Status(
                code=span.status.status_code.value,
                message=span.status.description or "",
            )
            
            # Create OTLP span
            otlp_span = Span(
                trace_id=ctx.trace_id.to_bytes(16, "big"),
                span_id=ctx.span_id.to_bytes(8, "big"),
                parent_span_id=parent.span_id.to_bytes(8, "big") if parent else b"",
                name=span.name,
                kind=span.kind.value + 1,  # OTLP enum starts at 1
                start_time_unix_nano=span.start_time,
                end_time_unix_nano=span.end_time or 0,
                attributes=attributes,
                events=events,
                status=status,
            )
            
            otlp_spans.append(otlp_span)
        
        # Create OTLP request
        return ExportTraceServiceRequest(
            resource_spans=[
                ResourceSpans(
                    resource=Resource(
                        attributes=[
                            KeyValue(
                                key="service.name",
                                value=AnyValue(string_value=self.service_name)
                            )
                        ]
                    ),
                    scope_spans=[
                        ScopeSpans(spans=otlp_spans)
                    ],
                )
            ]
        )
    
    def _value_to_otlp(self, value) -> AnyValue:
        """Convert a value to OTLP AnyValue."""
        if isinstance(value, bool):
            return AnyValue(bool_value=value)
        elif isinstance(value, int):
            return AnyValue(int_value=value)
        elif isinstance(value, float):
            return AnyValue(double_value=value)
        elif isinstance(value, str):
            return AnyValue(string_value=value)
        else:
            return AnyValue(string_value=str(value))
