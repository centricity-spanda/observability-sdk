"""Kafka handler for Python logging."""

import sys
import threading
from queue import Queue, Full
from typing import Optional

import orjson
from kafka import KafkaProducer
from kafka.errors import KafkaError

from observability.logging.config import LogConfig


class KafkaHandler:
    """Async Kafka handler for structured logging."""
    
    def __init__(self, config: LogConfig):
        self.config = config
        self.topic = config.log_topic
        self._producer: Optional[KafkaProducer] = None
        self._queue: Queue = Queue(maxsize=config.buffer_size)
        self._closed = False
        self._lock = threading.Lock()
        
        if config.kafka_brokers and not config.is_development:
            self._init_producer()
            self._start_sender_thread()
    
    def _init_producer(self):
        """Initialize Kafka producer."""
        try:
            acks = "all" if self.config.is_audit_log else 1
            self._producer = KafkaProducer(
                bootstrap_servers=self.config.kafka_brokers,
                acks=acks,
                compression_type="snappy",
                value_serializer=lambda v: orjson.dumps(v),
                retries=10 if self.config.is_audit_log else 3,
            )
        except KafkaError as e:
            sys.stderr.write(f"Failed to initialize Kafka producer: {e}\n")
            self._producer = None
    
    def _start_sender_thread(self):
        """Start background thread for sending logs."""
        if self._producer:
            thread = threading.Thread(target=self._sender_loop, daemon=True)
            thread.start()
    
    def _sender_loop(self):
        """Background loop to send queued logs."""
        while not self._closed:
            try:
                log_record = self._queue.get(timeout=1.0)
                if log_record and self._producer:
                    self._producer.send(self.topic, value=log_record)
            except Exception:
                pass  # Queue timeout or send error
    
    def emit(self, log_dict: dict) -> bool:
        """Send log record to Kafka.
        
        Returns True if queued, False if fallback to stderr.
        """
        if self._closed or not self._producer:
            return False
        
        try:
            self._queue.put_nowait(log_dict)
            return True
        except Full:
            # Queue full, fallback to stderr
            sys.stderr.write(orjson.dumps(log_dict).decode() + "\n")
            return False
    
    def close(self):
        """Close the Kafka producer."""
        with self._lock:
            self._closed = True
            if self._producer:
                self._producer.flush()
                self._producer.close()
