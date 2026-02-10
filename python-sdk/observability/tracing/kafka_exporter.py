"""Kafka span exporter for OpenTelemetry."""

import os
import sys
import threading
from queue import Queue, Full
from typing import Optional, Sequence

import orjson
from kafka import KafkaProducer
from kafka.errors import KafkaError
from opentelemetry.sdk.trace import ReadableSpan
from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult


class KafkaSpanExporter(SpanExporter):
    """Exports OpenTelemetry spans to Kafka."""
    
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
                compression_type="snappy",
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
                        key=span_data.get("trace_id", "").encode(),
                        value=orjson.dumps(span_data),
                    )
            except Exception:
                pass
    
    def export(self, spans: Sequence[ReadableSpan]) -> SpanExportResult:
        """Export spans to Kafka."""
        if self._closed or not self._producer:
            return SpanExportResult.FAILURE
        
        for span in spans:
            span_data = self._span_to_dict(span)
            try:
                self._queue.put_nowait(span_data)
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
    
    def _span_to_dict(self, span: ReadableSpan) -> dict:
        """Convert span to dictionary for JSON serialization."""
        ctx = span.get_span_context()
        parent = span.parent
        
        # Convert attributes
        attrs = {}
        if span.attributes:
            for key, value in span.attributes.items():
                attrs[key] = value
        
        # Convert events
        events = []
        if span.events:
            for event in span.events:
                event_attrs = {}
                if event.attributes:
                    for key, value in event.attributes.items():
                        event_attrs[key] = value
                events.append({
                    "name": event.name,
                    "timestamp": event.timestamp,
                    "attributes": event_attrs,
                })
        
        return {
            "trace_id": format(ctx.trace_id, "032x"),
            "span_id": format(ctx.span_id, "016x"),
            "parent_id": format(parent.span_id, "016x") if parent else "",
            "name": span.name,
            "kind": span.kind.name if span.kind else "INTERNAL",
            "start_time": span.start_time,
            "end_time": span.end_time,
            "duration_ms": (span.end_time - span.start_time) / 1_000_000 if span.end_time else 0,
            "status": span.status.status_code.name if span.status else "UNSET",
            "service": self.service_name,
            "attributes": attrs,
            "events": events,
        }
