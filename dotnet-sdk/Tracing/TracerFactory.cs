using System;
using System.Collections.Generic;
using System.Diagnostics;
using OpenTelemetry;
using OpenTelemetry.Resources;
using OpenTelemetry.Trace;

namespace Centricity.Observability.Tracing;

/// <summary>
/// Factory for setting up OpenTelemetry distributed tracing in .NET services.
///
/// Usage (ASP.NET Core):
/// <code>
///   builder.Services.AddOpenTelemetry()
///       .WithTracing(b => TracerFactory.Configure(b, "order-service"));
///
///   // Then obtain a tracer anywhere:
///   var tracer = TracerFactory.GetTracer("order-service");
/// </code>
/// </summary>
public static class TracerFactory
{
    private static ActivitySource? _source;

    /// <summary>
    /// Configures OpenTelemetry tracing with OTLP gRPC/HTTP exporter.
    /// Call this inside <c>builder.Services.AddOpenTelemetry().WithTracing(b => ...)</c>.
    /// </summary>
    public static TracerProviderBuilder Configure(TracerProviderBuilder builder, string serviceName)
    {
        var serviceVersion  = GetEnv("SERVICE_VERSION", "unknown");
        var environment     = GetEnvMulti(["ENVIRONMENT", "ENV"], "production");
        var samplingRate    = ParseDouble(GetEnv("TRACE_SAMPLING_RATE", "1.0"), 1.0);
        var otlpEndpoint    = GetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "");
        var otlpProtocol    = GetEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc"); // "grpc" | "http/protobuf"

        _source = new ActivitySource(serviceName, serviceVersion);

        builder
            .SetResourceBuilder(
                ResourceBuilder.CreateDefault()
                    .AddService(
                        serviceName:    serviceName,
                        serviceVersion: serviceVersion,
                        autoGenerateServiceInstanceId: true)
                    .AddAttributes(new Dictionary<string, object>
                    {
                        ["deployment.environment"] = environment,
                    }))
            .SetSampler(new ParentBasedSampler(new TraceIdRatioBasedSampler(samplingRate)))
            .AddSource(serviceName)
            .AddAspNetCoreInstrumentation()
            .AddHttpClientInstrumentation();

        // OTLP exporter
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

    /// <summary>
    /// Returns an <see cref="ActivitySource"/> for the given service (creates one if needed).
    /// Use this to start spans: <c>using var span = tracer.StartActivity("OperationName");</c>
    /// </summary>
    public static ActivitySource GetTracer(string serviceName)
    {
        _source ??= new ActivitySource(serviceName, GetEnv("SERVICE_VERSION", "unknown"));
        return _source;
    }

    // ── helpers ───────────────────────────────────────────────────────────────

    private static string GetEnv(string key, string defaultValue) =>
        System.Environment.GetEnvironmentVariable(key) is { Length: > 0 } v ? v : defaultValue;

    private static string GetEnvMulti(string[] keys, string defaultValue)
    {
        foreach (var k in keys)
            if (System.Environment.GetEnvironmentVariable(k) is { Length: > 0 } v)
                return v;
        return defaultValue;
    }

    private static double ParseDouble(string raw, double def) =>
        double.TryParse(raw, System.Globalization.NumberStyles.Float,
            System.Globalization.CultureInfo.InvariantCulture, out var d) ? d : def;
}
