"""Kafka metrics pusher."""

import os
import sys
import threading
import time
from typing import Optional

import grpc
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

from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2_grpc import (
    MetricsServiceStub,
)

from observability.metrics.registry import Registry


class MetricsPusher:
    """Periodically pushes Prometheus metrics to an OTEL collector via OTLP gRPC."""
    
    def __init__(
        self,
        service_name: str,
        otlp_endpoint: str,
        interval: float,
    ):
        self.service_name = service_name
        self.otlp_endpoint = otlp_endpoint
        self.interval = interval
        self._channel: Optional[grpc.Channel] = None
        self._stub: Optional[MetricsServiceStub] = None
        self._stop_event = threading.Event()
        self._thread: Optional[threading.Thread] = None
        
        self._init_client()
    
    def _init_client(self):
        """Initialize OTLP gRPC client."""
        try:
            self._channel = grpc.insecure_channel(self.otlp_endpoint)
            self._stub = MetricsServiceStub(self._channel)
        except Exception as e:
            sys.stderr.write(f"Failed to initialize OTLP metrics client: {e}\n")
            self._channel = None
            self._stub = None
    
    def start(self):
        """Start the background push thread."""
        if self._stub:
            self._thread = threading.Thread(target=self._push_loop, daemon=True)
            self._thread.start()
    
    def stop(self):
        """Stop the pusher and flush remaining metrics."""
        self._stop_event.set()
        if self._thread:
            self._thread.join(timeout=5.0)
        if self._channel:
            self._channel.close()
    
    def _push_loop(self):
        """Background loop to push metrics."""
        while not self._stop_event.wait(self.interval):
            self._push()
    
    def _push(self):
        """Push current metrics to OTEL collector in OTLP protobuf format."""
        if not self._stub:
            return
        
        try:
            # Convert Prometheus metrics to OTLP format
            otlp_request = self._convert_to_otlp()
            
            # Send via OTLP gRPC
            self._stub.Export(otlp_request)
        except Exception as e:
            sys.stderr.write(f"Failed to push metrics: {e}\n")
    
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
    
    interval_str = os.getenv("METRICS_PUSH_INTERVAL", "15")
    
    # Parse interval (remove 's' suffix if present)
    try:
        interval = float(interval_str.rstrip("s"))
    except ValueError:
        interval = 15.0
    
    # Determine OTLP endpoint (collector agent)
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
    if "://" in endpoint:
        endpoint = endpoint.split("://", 1)[1]

    _pusher = MetricsPusher(service_name, endpoint, interval)
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
