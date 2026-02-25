# Span Status Utilities

Both the Python and Go SDKs now re-export OpenTelemetry's span status utilities, so you don't need to import them directly from OpenTelemetry packages.

## Python SDK

### Before
```python
from observability import new_tracer
from opentelemetry import trace  # Had to import separately

tracer = new_tracer("my-service")

with tracer.start_as_current_span("operation") as span:
    try:
        # ... operation ...
    except Exception as e:
        span.record_exception(e)
        span.set_status(trace.Status(trace.StatusCode.ERROR, str(e)))
```

### After
```python
from observability import new_tracer, Status, StatusCode  # All from SDK

tracer = new_tracer("my-service")

with tracer.start_as_current_span("operation") as span:
    try:
        # ... operation ...
    except Exception as e:
        span.record_exception(e)
        span.set_status(Status(StatusCode.ERROR, str(e)))
```

## Go SDK

### Before
```go
import (
    obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/codes"  // Had to import separately
)

func someOperation(ctx context.Context) error {
    tracer := otel.Tracer("my-service")
    ctx, span := tracer.Start(ctx, "operation")
    defer span.End()
    
    if err := doWork(); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return err
    }
    return nil
}
```

### After
```go
import (
    obs "github.com/centricitywealthtech/platform-observability-sdk/go-sdk/pkg/observability"
    "go.opentelemetry.io/otel"
)

func someOperation(ctx context.Context) error {
    tracer := otel.Tracer("my-service")
    ctx, span := tracer.Start(ctx, "operation")
    defer span.End()
    
    if err := doWork(); err != nil {
        span.RecordError(err)
        span.SetStatus(obs.StatusCodeError, err.Error())  // From SDK
        return err
    }
    return nil
}
```

## Available Status Codes

### Python
- `StatusCode.UNSET` - Default state
- `StatusCode.OK` - Operation succeeded
- `StatusCode.ERROR` - Operation failed

### Go
- `obs.StatusCodeUnset` - Default state
- `obs.StatusCodeOK` - Operation succeeded
- `obs.StatusCodeError` - Operation failed

## Best Practices

1. **Always set ERROR status on exceptions**:
   ```python
   except Exception as e:
       span.record_exception(e)  # Records full exception details
       span.set_status(Status(StatusCode.ERROR, str(e)))  # Marks span as failed
   ```

2. **Provide meaningful descriptions**:
   ```python
   span.set_status(Status(StatusCode.ERROR, "Failed to connect to database"))
   ```

3. **Don't set OK explicitly** (it's implied if no error):
   ```python
   # This is unnecessary:
   span.set_status(Status(StatusCode.OK, "Success"))
   
   # Just let it default to UNSET/OK
   ```

## Benefits

- **Simplified imports**: One-stop import from the SDK
- **Better visibility**: Failed spans are highlighted in Jaeger/tracing UIs
- **Filtering**: Query traces by status (e.g., show only errors)
- **Alerting**: Monitor error rates across services
- **Debugging**: Quickly identify where operations failed
