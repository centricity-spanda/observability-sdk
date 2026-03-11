// .NET Example Service — Full observability: ILogger (Serilog), Traces (OTel), Metrics (OTel+Prometheus)
//
// Integration pattern (same as any real Centricity project):
// 1. ObservabilityLogger.Create()       → Serilog with EnvelopeSink (logs)
// 2. TracerFactory.Configure()          → OpenTelemetry tracing with OTLP export
// 3. MetricsFactory.Configure()         → OpenTelemetry metrics with Prometheus scrape
// 4. builder.Host.UseSerilog()          → wire Serilog as ILogger provider
// 5. app.UseMiddleware<HttpMetrics>()   → auto RED metrics per request
// 6. app.UseMiddleware<HttpTracing>()   → auto spans per request
// 7. app.MapPrometheusScrapingEndpoint()→ expose /metrics for scraping
//
// Developers just use ILogger normally. Traces and metrics are auto-collected.

using System.Diagnostics;
using Serilog;
using OpenTelemetry;
using Centricity.Observability.Logging;
using Centricity.Observability.Tracing;
using Centricity.Observability.Metrics;

var serviceName = Environment.GetEnvironmentVariable("SERVICE_NAME") ?? "dotnet-service";

// ── Step 1: Create the observability logger (Serilog with EnvelopeSink) ────
var (seriLogger, seriSink) = ObservabilityLogger.CreateWithSink(serviceName);

var builder = WebApplication.CreateBuilder(args);

// ── Step 2: Wire Serilog as the ILogger provider ───────────────────────────
builder.Host.UseSerilog();

// ── Step 3: Add OpenTelemetry tracing + metrics ────────────────────────────
builder.Services.AddOpenTelemetry()
    .WithTracing(b => TracerFactory.Configure(b, serviceName))
    .WithMetrics(b => MetricsFactory.Configure(b, serviceName));

var app = builder.Build();

// ── Step 4: Register auto HTTP middleware ───────────────────────────────────
app.UseMiddleware<HttpMetricsMiddleware>();    // auto http_requests_total/duration/in_flight
app.UseMiddleware<HttpTracingMiddleware>();    // auto spans per request

// ── Step 5: Expose /metrics for Prometheus scraping ────────────────────────
app.MapPrometheusScrapingEndpoint();

// ── Step 6: Graceful shutdown — flush Serilog + dispose sink ───────────────
app.Lifetime.ApplicationStopping.Register(() =>
{
    Log.Information("Application shutting down — flushing logs");
    Log.CloseAndFlush();
    seriSink.Dispose();
});

// Get the ActivitySource for manual spans
var tracer = TracerFactory.GetTracer(serviceName);

// ── Health Check ───────────────────────────────────────────────────────────
app.MapGet("/health", () => Results.Ok(new { status = "healthy", service = serviceName }));

// ── POST /api/orders — Normal order creation ──────────────────────────────
app.MapPost("/api/orders", async (HttpRequest request, ILogger<Program> logger) =>
{
    // Manual child span (auto parent from middleware)
    using var span = tracer.StartActivity("CreateOrder");

    string body;
    using (var reader = new StreamReader(request.Body))
        body = await reader.ReadToEndAsync();

    // trace_id, span_id auto-injected by EnvelopeSink
    logger.LogInformation("Order creation started, Body: {RequestBody}", body);

    // Simulate DB call
    using (var dbSpan = tracer.StartActivity("DB.InsertOrder"))
    {
        await Task.Delay(Random.Shared.Next(50, 200));
        dbSpan?.SetTag("db.system", "postgresql");
        dbSpan?.SetTag("db.operation", "INSERT");
    }

    var orderId = $"ORD-{DateTime.UtcNow:yyyyMMdd}-{Random.Shared.Next(10000, 99999)}";
    span?.SetTag("order.id", orderId);

    logger.LogInformation("Order created successfully, OrderId: {OrderId}, Status: {HttpStatusCode}",
        orderId, 201);

    return Results.Created($"/api/orders/{orderId}", new { order_id = orderId, status = "created" });
});

// ── GET /api/orders/{id} — Fetch order ────────────────────────────────────
app.MapGet("/api/orders/{id}", async (string id, ILogger<Program> logger) =>
{
    using var span = tracer.StartActivity("GetOrder");
    span?.SetTag("order.id", id);

    logger.LogInformation("Fetching order, OrderId: {OrderId}", id);

    // Simulate DB lookup
    using (var dbSpan = tracer.StartActivity("DB.SelectOrder"))
    {
        await Task.Delay(Random.Shared.Next(10, 50));
        dbSpan?.SetTag("db.system", "postgresql");
        dbSpan?.SetTag("db.operation", "SELECT");
    }

    return Results.Ok(new { order_id = id, status = "completed", amount = 1500.50 });
});

// ── POST /api/error — Trigger 500 errors (for alerting demo) ──────────────
app.MapPost("/api/error", (HttpRequest request, ILogger<Program> logger) =>
{
    var count = 1;
    if (request.Query.ContainsKey("count") && int.TryParse(request.Query["count"], out var c))
        count = c;

    using var span = tracer.StartActivity("SimulateError");
    span?.SetTag("error", true);
    span?.SetTag("error.count", count);

    try
    {
        throw new InvalidOperationException("Simulated application error for alerting demo");
    }
    catch (Exception ex)
    {
        span?.SetStatus(ActivityStatusCode.Error, ex.Message);
        span?.RecordException(ex);

        // ILogger.LogError with exception — EnvelopeSink fills error block
        logger.LogError(ex, "Simulated error triggered, ErrorCount: {ErrorCount}", count);
    }

    return Results.Json(new { error = "demo error", count }, statusCode: 500);
});

// ── POST /api/stress — CPU/memory burn (for latency alerting) ─────────────
app.MapPost("/api/stress", (HttpRequest request, ILogger<Program> logger) =>
{
    var durationMs = 2000;
    var memoryMb = 50;
    if (request.Query.ContainsKey("duration_ms") && int.TryParse(request.Query["duration_ms"], out var d))
        durationMs = d;
    if (request.Query.ContainsKey("memory_mb") && int.TryParse(request.Query["memory_mb"], out var m))
        memoryMb = m;

    using var span = tracer.StartActivity("StressTest");
    span?.SetTag("stress.duration_ms", durationMs);
    span?.SetTag("stress.memory_mb", memoryMb);

    logger.LogInformation("Stress test started, DurationMs: {DurationMs}, MemoryMb: {MemoryMb}",
        durationMs, memoryMb);

    var sw = Stopwatch.StartNew();
    var deadline = sw.ElapsedMilliseconds + durationMs;
    while (sw.ElapsedMilliseconds < deadline) { }
    byte[]? chunk = memoryMb > 0 ? new byte[memoryMb * 1024 * 1024] : null;
    sw.Stop();

    var elapsed = Math.Round(sw.Elapsed.TotalSeconds, 2);
    span?.SetTag("stress.elapsed_seconds", elapsed);

    logger.LogInformation("Stress test completed, ElapsedSeconds: {ElapsedSeconds}", elapsed);

    GC.KeepAlive(chunk);
    return Results.Ok(new { duration_ms = durationMs, memory_mb = memoryMb, elapsed_seconds = elapsed });
});

// ── POST /api/slow — Artificially slow (for P99 alerting) ────────────────
app.MapPost("/api/slow", async (HttpRequest request, ILogger<Program> logger) =>
{
    var delayMs = 3000;
    if (request.Query.ContainsKey("delay_ms") && int.TryParse(request.Query["delay_ms"], out var d))
        delayMs = d;

    using var span = tracer.StartActivity("SlowRequest");
    span?.SetTag("delay_ms", delayMs);

    logger.LogWarning("Slow request processing, DelayMs: {DelayMs}", delayMs);

    await Task.Delay(delayMs);

    return Results.Ok(new { message = "slow response", delay_ms = delayMs });
});

// ── GET /api/users — PII redaction test ───────────────────────────────────
app.MapGet("/api/users", (ILogger<Program> logger) =>
{
    using var span = tracer.StartActivity("PIIRedactionTest");

    // Log sensitive data — EnvelopeSink + PiiRedactor will mask automatically
    logger.LogInformation(
        "User profile accessed, " +
        "Password: {Password}, Token: {Token}, " +
        "Email: {Email}, Phone: {Phone}, " +
        "Pan: {Pan}, Aadhaar: {Aadhaar}, Card: {Card}",
        "secret123", "bearer-xyz",
        "john@example.com", "+919876543210",
        "ABCDE1234F", "8561 0272 7756", "4111-1111-1111-1111");

    return Results.Ok(new { user_id = "USR-001", name = "John Doe" });
});

app.Run();

// Required for ILogger<Program> injection in minimal APIs
public partial class Program { }

