"""Kafka metrics pusher."""

import os
import sys
import threading
import time
from typing import Optional

from kafka import KafkaProducer
from kafka.errors import KafkaError
from prometheus_client import REGISTRY
from prometheus_client.core import (
    CounterMetricFamily,
    GaugeMetricFamily,
    HistogramMetricFamily,
    SummaryMetricFamily,
)

from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import (
    ExportMetricsServiceRequest,
)
from opentelemetry.proto.common.v1.common_pb2 import AnyValue, KeyValue
from opentelemetry.proto.metrics.v1.metrics_pb2 import (
    AggregationTemporality,
    Gauge,
    Histogram,
    HistogramDataPoint,
    Metric,
    NumberDataPoint,
    ResourceMetrics,
    ScopeMetrics,
    Sum,
    Summary,
    SummaryDataPoint,
)
from opentelemetry.proto.resource.v1.resource_pb2 import Resource

from observability.metrics.registry import Registry, kafka_producer_messages_total


class MetricsPusher:
    """Periodically pushes Prometheus metrics to Kafka in OTLP format."""
    
    def __init__(
        self,
        service_name: str,
        kafka_brokers: list[str],
        topic: str,
        interval: float,
    ):
        self.service_name = service_name
        self.kafka_brokers = kafka_brokers
        self.topic = topic
        self.interval = interval
        self._producer: Optional[KafkaProducer] = None
        self._stop_event = threading.Event()
        self._thread: Optional[threading.Thread] = None
        
        self._init_producer()
    
    def _init_producer(self):
        """Initialize Kafka producer."""
        try:
            self._producer = KafkaProducer(
                bootstrap_servers=self.kafka_brokers,
                acks=1,
                compression_type="gzip",
            )
        except KafkaError as e:
            sys.stderr.write(f"Failed to initialize metrics Kafka producer: {e}\n")
            self._producer = None
    
    def start(self):
        """Start the background push thread."""
        if self._producer:
            self._thread = threading.Thread(target=self._push_loop, daemon=True)
            self._thread.start()
    
    def stop(self):
        """Stop the pusher and flush remaining metrics."""
        self._stop_event.set()
        if self._thread:
            self._thread.join(timeout=5.0)
        if self._producer:
            self._push()  # Final push
            self._producer.close()
    
    def _push_loop(self):
        """Background loop to push metrics."""
        while not self._stop_event.wait(self.interval):
            self._push()
    
    def _push(self):
        """Push current metrics to Kafka in OTLP protobuf format."""
        if not self._producer:
            return
        
        try:
            # Convert Prometheus metrics to OTLP format
            otlp_request = self._convert_to_otlp()
            
            # Serialize to protobuf
            metrics_data = otlp_request.SerializeToString()
            
            # Send to Kafka
            self._producer.send(
                self.topic,
                key=self.service_name.encode(),
                value=metrics_data,
            )
            self._producer.flush()
            
            kafka_producer_messages_total.labels(
                service=self.service_name,
                topic=self.topic,
                status="success",
            ).inc()
        except Exception as e:
            sys.stderr.write(f"Failed to push metrics: {e}\n")
            kafka_producer_messages_total.labels(
                service=self.service_name,
                topic=self.topic,
                status="error",
            ).inc()
    
    def _convert_to_otlp(self) -> ExportMetricsServiceRequest:
        """Convert Prometheus metrics to OTLP format."""
        timestamp_nanos = int(time.time() * 1e9)
        metrics = []
        
        # Iterate through metric families
        for collector in Registry._collector_to_names.keys():
            for metric_family in collector.collect():
                metric_name = metric_family.name
                metric_type = metric_family.type
                metric_help = metric_family.documentation
                
                # Create metric based on type
                if metric_type == "counter":
                    for sample in metric_family.samples:
                        attributes = [
                            KeyValue(
                                key=label_name,
                                value=AnyValue(string_value=str(label_value))
                            )
                            for label_name, label_value in sample.labels.items()
                        ]
                        metric = Metric(
                            name=sample.name,
                            description=metric_help,
                            sum=Sum(
                                aggregation_temporality=AggregationTemporality.AGGREGATION_TEMPORALITY_CUMULATIVE,
                                is_monotonic=True,
                                data_points=[
                                    NumberDataPoint(
                                        attributes=attributes,
                                        time_unix_nano=timestamp_nanos,
                                        as_double=float(sample.value),
                                    )
                                ],
                            ),
                        )
                        metrics.append(metric)
                
                elif metric_type == "gauge":
                    for sample in metric_family.samples:
                        attributes = [
                            KeyValue(
                                key=label_name,
                                value=AnyValue(string_value=str(label_value))
                            )
                            for label_name, label_value in sample.labels.items()
                        ]
                        metric = Metric(
                            name=sample.name,
                            description=metric_help,
                            gauge=Gauge(
                                data_points=[
                                    NumberDataPoint(
                                        attributes=attributes,
                                        time_unix_nano=timestamp_nanos,
                                        as_double=float(sample.value),
                                    )
                                ],
                            ),
                        )
                        metrics.append(metric)
                
                elif metric_type == "histogram":
                    # Group samples by label set (excluding 'le')
                    groups: dict[str, dict] = {}
                    for sample in metric_family.samples:
                        labels_copy = {k: v for k, v in sample.labels.items() if k != "le"}
                        key = str(sorted(labels_copy.items()))
                        
                        if key not in groups:
                            groups[key] = {
                                "labels": labels_copy,
                                "bucket_bounds": [],
                                "bucket_counts": [],
                                "count": 0,
                                "sum_value": 0.0,
                            }
                        group = groups[key]
                        
                        if sample.name.endswith("_bucket"):
                            le = sample.labels.get("le", "")
                            if le != "+Inf":
                                group["bucket_bounds"].append(float(le))
                                group["bucket_counts"].append(int(sample.value))
                            else:
                                # +Inf bucket — overflow count
                                group["bucket_counts"].append(int(sample.value))
                        elif sample.name.endswith("_count"):
                            group["count"] = int(sample.value)
                        elif sample.name.endswith("_sum"):
                            group["sum_value"] = float(sample.value)
                    
                    for group in groups.values():
                        attributes = [
                            KeyValue(
                                key=label_name,
                                value=AnyValue(string_value=str(label_value))
                            )
                            for label_name, label_value in group["labels"].items()
                        ]
                        metric = Metric(
                            name=metric_name,
                            description=metric_help,
                            histogram=Histogram(
                                aggregation_temporality=AggregationTemporality.AGGREGATION_TEMPORALITY_CUMULATIVE,
                                data_points=[
                                    HistogramDataPoint(
                                        attributes=attributes,
                                        time_unix_nano=timestamp_nanos,
                                        count=group["count"],
                                        sum=group["sum_value"],
                                        bucket_counts=group["bucket_counts"],
                                        explicit_bounds=group["bucket_bounds"],
                                    )
                                ],
                            ),
                        )
                        metrics.append(metric)
                
                elif metric_type == "summary":
                    # Group samples by label set (excluding 'quantile')
                    groups = {}
                    for sample in metric_family.samples:
                        labels_copy = {k: v for k, v in sample.labels.items() if k != "quantile"}
                        key = str(sorted(labels_copy.items()))
                        
                        if key not in groups:
                            groups[key] = {
                                "labels": labels_copy,
                                "quantiles": [],
                                "count": 0,
                                "sum_value": 0.0,
                            }
                        group = groups[key]
                        
                        if sample.name.endswith("_count"):
                            group["count"] = int(sample.value)
                        elif sample.name.endswith("_sum"):
                            group["sum_value"] = float(sample.value)
                        elif "quantile" in sample.labels:
                            group["quantiles"].append(
                                SummaryDataPoint.ValueAtQuantile(
                                    quantile=float(sample.labels["quantile"]),
                                    value=float(sample.value),
                                )
                            )
                    
                    for group in groups.values():
                        attributes = [
                            KeyValue(
                                key=label_name,
                                value=AnyValue(string_value=str(label_value))
                            )
                            for label_name, label_value in group["labels"].items()
                        ]
                        metric = Metric(
                            name=metric_name,
                            description=metric_help,
                            summary=Summary(
                                data_points=[
                                    SummaryDataPoint(
                                        attributes=attributes,
                                        time_unix_nano=timestamp_nanos,
                                        count=group["count"],
                                        sum=group["sum_value"],
                                        quantile_values=group["quantiles"],
                                    )
                                ],
                            ),
                        )
                        metrics.append(metric)
        
        # Create the OTLP request
        return ExportMetricsServiceRequest(
            resource_metrics=[
                ResourceMetrics(
                    resource=Resource(
                        attributes=[
                            KeyValue(
                                key="service.name",
                                value=AnyValue(string_value=self.service_name)
                            )
                        ]
                    ),
                    scope_metrics=[
                        ScopeMetrics(metrics=metrics)
                    ],
                )
            ]
        )



_pusher: Optional[MetricsPusher] = None


def start_metrics_pusher(service_name: str) -> bool:
    """Start the metrics pusher.
    
    Returns True if started, False if disabled or failed.
    """
    global _pusher
    
    # Skip in development mode
    if _get_env("ENVIRONMENT", "production") == "development":
        return False
    
    # Check if Kafka export is enabled
    if os.getenv("METRICS_KAFKA_ENABLED", "true").lower() not in ("true", "1", "yes"):
        return False
    
    # Get configuration from environment
    brokers = os.getenv("KAFKA_BROKERS", "")
    if not brokers:
        sys.stderr.write("Warning: KAFKA_BROKERS not set, metrics push disabled\n")
        return False
    
    kafka_brokers = [b.strip() for b in brokers.split(",")]
    topic = os.getenv("KAFKA_METRICS_TOPIC", "metrics.application")
    interval_str = os.getenv("METRICS_PUSH_INTERVAL", "15")
    
    # Parse interval (remove 's' suffix if present)
    try:
        interval = float(interval_str.rstrip("s"))
    except ValueError:
        interval = 15.0
    
    _pusher = MetricsPusher(service_name, kafka_brokers, topic, interval)
    _pusher.start()
    
    return True


def stop_metrics_pusher():
    """Stop the metrics pusher."""
    global _pusher
    if _pusher:
        _pusher.stop()
        _pusher = None


def _get_env(key: str, default: str) -> str:
    """Get environment variable with fallback names for ENVIRONMENT."""
    if key == "ENVIRONMENT":
        for k in ["ENVIRONMENT", "ENV", "environment", "env"]:
            v = os.getenv(k)
            if v:
                return v
    
    return os.getenv(key, default)
