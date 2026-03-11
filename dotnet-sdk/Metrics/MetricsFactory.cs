using System;
using System.Collections.Generic;
using System.Diagnostics.Metrics;
using OpenTelemetry;
using OpenTelemetry.Metrics;
using OpenTelemetry.Resources;

namespace Centricity.Observability.Metrics;

/// <summary>
/// Factory for setting up OpenTelemetry metrics with Prometheus scrape endpoint
/// and optional OTLP push exporter.
///
/// Usage (ASP.NET Core):
/// <code>
///   builder.Services.AddOpenTelemetry()
///       .WithMetrics(b => MetricsFactory.Configure(b, "order-service"));
///
///   // In your pipeline:
///   app.MapPrometheusScrapingEndpoint();   // /metrics
/// </code>
/// </summary>
public static class MetricsFactory
{
    // ── Pre-registered HTTP instruments (matching Go / Python / Node SDKs) ────

    private static Meter? _meter;

    private static Counter<long>?       _httpRequestsTotal;
    private static Histogram<double>?   _httpRequestDurationSeconds;
    private static UpDownCounter<long>? _httpRequestsInFlight;

    /// <summary>
    /// Configures OpenTelemetry metrics: Prometheus scrape + optional OTLP push.
    /// Call inside <c>builder.Services.AddOpenTelemetry().WithMetrics(b => ...)</c>.
    /// </summary>
    public static MeterProviderBuilder Configure(MeterProviderBuilder builder, string serviceName)
    {
        var serviceVersion = GetEnv("SERVICE_VERSION", "unknown");
        var environment    = GetEnvMulti(["ENVIRONMENT", "ENV"], "production");
        var otlpEndpoint   = GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "");
        var otlpProtocol   = GetEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc");

        _meter = new Meter(serviceName, serviceVersion);
        InitInstruments();

        builder
            .SetResourceBuilder(
                ResourceBuilder.CreateDefault()
                    .AddService(serviceName: serviceName, serviceVersion: serviceVersion)
                    .AddAttributes(new Dictionary<string, object>
                    {
                        ["deployment.environment"] = environment,
                    }))
            .AddMeter(serviceName)
            .AddRuntimeInstrumentation()
            .AddAspNetCoreInstrumentation()
            // Prometheus scrape endpoint (/metrics by default)
            .AddPrometheusExporter();

        // Optional OTLP push exporter (e.g. to OTEL collector)
        if (!string.IsNullOrEmpty(otlpEndpoint))
        {
            var protocol = otlpProtocol.Equals("http/protobuf", StringComparison.OrdinalIgnoreCase)
                ? OpenTelemetry.Exporter.OtlpExportProtocol.HttpProtobuf
                : OpenTelemetry.Exporter.OtlpExportProtocol.Grpc;

            builder.AddOtlpExporter(o =>
            {
                o.Endpoint = new Uri(otlpEndpoint);
                o.Protocol = protocol;
            });
        }

        return builder;
    }

    // ── HTTP metric helpers (align with Go / Python / Node SDKs) ─────────────

    /// <summary>Records a completed HTTP request (updates total, duration, in-flight).</summary>
    public static void RecordHttpRequest(
        string serviceName,
        string method,
        string path,
        int    statusCode,
        double durationSeconds)
    {
        var tags = new TagList
        {
            { "service", serviceName },
            { "method",  method      },
            { "path",    path        },
        };

        _httpRequestDurationSeconds?.Record(durationSeconds, tags);
        _httpRequestsInFlight?.Add(-1, new TagList { { "service", serviceName } });

        tags.Add("status", statusCode.ToString());
        _httpRequestsTotal?.Add(1, tags);
    }

    /// <summary>Called when an HTTP request starts (increments in-flight gauge).</summary>
    public static void OnHttpRequestStart(string serviceName) =>
        _httpRequestsInFlight?.Add(1, new TagList { { "service", serviceName } });

    // ── Meter accessor ────────────────────────────────────────────────────────

    /// <summary>Returns the shared <see cref="Meter"/> for custom instruments.</summary>
    public static Meter GetMeter(string serviceName, string version = "")
    {
        _meter ??= new Meter(serviceName, string.IsNullOrEmpty(version) ? GetEnv("SERVICE_VERSION", "unknown") : version);
        return _meter;
    }

    // ── Private ───────────────────────────────────────────────────────────────

    private static void InitInstruments()
    {
        if (_meter is null) return;

        _httpRequestsTotal = _meter.CreateCounter<long>(
            "http_requests_total",
            description: "Total number of HTTP requests");

        _httpRequestDurationSeconds = _meter.CreateHistogram<double>(
            "http_request_duration_seconds",
            unit: "s",
            description: "HTTP request duration in seconds");

        _httpRequestsInFlight = _meter.CreateUpDownCounter<long>(
            "http_requests_in_flight",
            description: "Current number of in-flight HTTP requests");
    }

    private static string GetEnv(string key, string defaultValue) =>
        System.Environment.GetEnvironmentVariable(key) is { Length: > 0 } v ? v : defaultValue;

    private static string GetEnvMulti(string[] keys, string defaultValue)
    {
        foreach (var k in keys)
            if (System.Environment.GetEnvironmentVariable(k) is { Length: > 0 } v)
                return v;
        return defaultValue;
    }
}
