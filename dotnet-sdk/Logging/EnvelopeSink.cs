using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using Confluent.Kafka;
using Serilog.Core;
using Serilog.Events;

namespace Centricity.Observability.Logging;

/// <summary>
/// Serilog <see cref="ILogEventSink"/> that serialises every log event into
/// the unified Centricity observability envelope JSON and writes it to
/// one or more transports (console, file, Kafka).
/// </summary>
public sealed class EnvelopeSink : ILogEventSink, IDisposable
{
    private readonly LogConfig _cfg;
    private readonly StreamWriter? _fileWriter;
    private readonly IProducer<Null, string>? _kafkaProducer;

    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.Never,
        WriteIndented = false,
    };

    public EnvelopeSink(LogConfig cfg)
    {
        _cfg = cfg;

        // File transport
        if (cfg.EnableFile)
        {
            try
            {
                var dir = Path.GetDirectoryName(cfg.LogFilePath);
                if (!string.IsNullOrEmpty(dir)) Directory.CreateDirectory(dir);
                _fileWriter = new StreamWriter(cfg.LogFilePath, append: true, encoding: System.Text.Encoding.UTF8)
                {
                    AutoFlush = true
                };
            }
            catch (Exception ex)
            {
                Console.Error.WriteLine($"[EnvelopeSink] Cannot open log file: {ex.Message}");
            }
        }

        // Kafka transport
        if (cfg.EnableKafka && cfg.KafkaBrokers.Count > 0)
        {
            try
            {
                var producerCfg = new ProducerConfig
                {
                    BootstrapServers = string.Join(",", cfg.KafkaBrokers),
                    Acks             = Acks.Leader,
                };
                _kafkaProducer = new ProducerBuilder<Null, string>(producerCfg).Build();
            }
            catch (Exception ex)
            {
                Console.Error.WriteLine($"[EnvelopeSink] Kafka init failed: {ex.Message}");
            }
        }
    }

    // ── ILogEventSink ─────────────────────────────────────────────────────────

    public void Emit(LogEvent logEvent)
    {
        var envelope = BuildEnvelope(logEvent);
        var line = JsonSerializer.Serialize(envelope, JsonOpts) + "\n";

        if (_cfg.EnableConsole) Console.Write(line);
        _fileWriter?.Write(line);
        PublishToKafka(line);
    }

    // ── Envelope builder ──────────────────────────────────────────────────────

    private EnvelopeLog BuildEnvelope(LogEvent logEvent)
    {
        // Severity
        var severity    = ToSeverityString(logEvent.Level);
        var severityNum = ToSeverityNum(logEvent.Level);

        // Service block
        var service = new Dictionary<string, string>
        {
            ["service.name"]            = _cfg.ServiceName,
            ["service.version"]         = _cfg.ServiceVersion,
            ["service.namespace"]       = _cfg.ServiceNamespace,
            ["deployment.environment"]  = _cfg.Environment,
        };
        if (!string.IsNullOrEmpty(_cfg.HostName))         service["host.name"]            = _cfg.HostName;
        if (!string.IsNullOrEmpty(_cfg.K8sPodName))       service["k8s.pod.name"]         = _cfg.K8sPodName;
        if (!string.IsNullOrEmpty(_cfg.K8sNamespaceName)) service["k8s.namespace.name"]    = _cfg.K8sNamespaceName;
        if (!string.IsNullOrEmpty(_cfg.K8sNodeName))      service["k8s.node.name"]         = _cfg.K8sNodeName;

        // Attributes
        var attributes = new Dictionary<string, object?>();

        // Auto-inject OTel trace context from Activity.Current
        InjectTraceContext(attributes);

        // Merge Serilog properties into attributes (caller-supplied take precedence)
        foreach (var (key, value) in logEvent.Properties)
        {
            // Skip internal Serilog properties
            if (key is "SourceContext" or "RequestId" or "RequestPath") continue;
            if (!attributes.ContainsKey(key))
                attributes[key] = ScalarOrString(value);
        }

        // Standard defaults
        if (!attributes.ContainsKey("log.type")) attributes["log.type"] = _cfg.LogType;
        if (!string.IsNullOrEmpty(_cfg.Team) && !attributes.ContainsKey("team"))
            attributes["team"] = _cfg.Team;

        // PII redaction
        if (_cfg.EnablePiiRedaction)
            attributes = PiiRedactor.Redact(attributes);

        // Error block
        var errorBlock = new Dictionary<string, object?>();
        if (logEvent.Exception is { } ex)
        {
            errorBlock["message"] = ex.Message;
            errorBlock["type"]    = ex.GetType().FullName;
            errorBlock["stack"]   = ex.StackTrace;
        }

        return new EnvelopeLog
        {
            Timestamp   = logEvent.Timestamp.UtcDateTime.ToString("o"),
            Severity    = severity,
            SeverityNum = severityNum,
            Message     = logEvent.RenderMessage(),
            Service     = service,
            Attributes  = attributes,
            Error       = errorBlock,
        };
    }

    // ── OTel trace context injection ──────────────────────────────────────────

    private static void InjectTraceContext(Dictionary<string, object?> attrs)
    {
        var activity = Activity.Current;
        if (activity is null) return;

        if (!attrs.ContainsKey("trace_id"))
            attrs["trace_id"] = activity.TraceId.ToString();

        if (!attrs.ContainsKey("span_id"))
            attrs["span_id"] = activity.SpanId.ToString();

        if (!attrs.ContainsKey("trace_flags"))
            attrs["trace_flags"] = ((int)activity.ActivityTraceFlags).ToString("x2");

        if (!attrs.ContainsKey("parent_span_id") && activity.ParentSpanId != default)
            attrs["parent_span_id"] = activity.ParentSpanId.ToString();
    }

    // ── Kafka ─────────────────────────────────────────────────────────────────

    private void PublishToKafka(string line)
    {
        if (_kafkaProducer is null) return;
        try
        {
            _ = _kafkaProducer.ProduceAsync(_cfg.KafkaTopic, new Message<Null, string> { Value = line });
        }
        catch
        {
            // best-effort; never throw from logging
        }
    }

    // ── Severity helpers ──────────────────────────────────────────────────────

    private static string ToSeverityString(LogEventLevel level) => level switch
    {
        LogEventLevel.Verbose     => "TRACE",
        LogEventLevel.Debug       => "DEBUG",
        LogEventLevel.Information => "INFO",
        LogEventLevel.Warning     => "WARN",
        LogEventLevel.Error       => "ERROR",
        LogEventLevel.Fatal       => "FATAL",
        _                         => "INFO",
    };

    private static int ToSeverityNum(LogEventLevel level) => level switch
    {
        LogEventLevel.Verbose     => 1,
        LogEventLevel.Debug       => 5,
        LogEventLevel.Information => 9,
        LogEventLevel.Warning     => 13,
        LogEventLevel.Error       => 17,
        LogEventLevel.Fatal       => 21,
        _                         => 9,
    };

    private static object? ScalarOrString(LogEventPropertyValue pv) =>
        pv is ScalarValue sv ? sv.Value : pv.ToString();

    // ── IDisposable ───────────────────────────────────────────────────────────

    public void Dispose()
    {
        _fileWriter?.Dispose();
        _kafkaProducer?.Flush(TimeSpan.FromSeconds(5));
        _kafkaProducer?.Dispose();
    }
}

// ── Envelope model (for JSON serialisation) ───────────────────────────────────

internal sealed class EnvelopeLog
{
    [JsonPropertyName("timestamp")]    public string Timestamp   { get; init; } = "";
    [JsonPropertyName("severity")]     public string Severity    { get; init; } = "";
    [JsonPropertyName("severity_num")] public int    SeverityNum { get; init; }
    [JsonPropertyName("message")]      public string Message     { get; init; } = "";
    [JsonPropertyName("service")]      public Dictionary<string, string>  Service    { get; init; } = [];
    [JsonPropertyName("attributes")]   public Dictionary<string, object?> Attributes { get; init; } = [];
    [JsonPropertyName("error")]        public Dictionary<string, object?> Error      { get; init; } = [];
}
