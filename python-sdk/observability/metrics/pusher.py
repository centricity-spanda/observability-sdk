"""Kafka metrics pusher."""

import os
import sys
import threading
import time
from typing import Optional

from kafka import KafkaProducer
from kafka.errors import KafkaError
from prometheus_client import generate_latest

from observability.metrics.registry import Registry, kafka_producer_messages_total


class MetricsPusher:
    """Periodically pushes Prometheus metrics to Kafka."""
    
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
                compression_type="snappy",
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
        """Push current metrics to Kafka."""
        if not self._producer:
            return
        
        try:
            # Generate Prometheus text format
            metrics_data = generate_latest(Registry)
            
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


_pusher: Optional[MetricsPusher] = None


def start_metrics_pusher(service_name: str) -> bool:
    """Start the metrics pusher.
    
    Returns True if started, False if disabled or failed.
    """
    global _pusher
    
    # Skip in development mode
    if os.getenv("ENVIRONMENT", "production") == "development":
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
