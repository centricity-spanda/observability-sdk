using System;
using System.Collections.Generic;

namespace Centricity.Observability.Logging;

/// <summary>
/// Configuration for the observability logger, loaded from environment variables.
/// Uses the same variable names as the Go and Python SDKs.
/// </summary>
public sealed class LogConfig
{
    // ── Service identity ──────────────────────────────────────────────────────
    public string ServiceName        { get; init; }
    public string ServiceVersion     { get; init; }
    public string ServiceNamespace   { get; init; }
    public string Environment        { get; init; }
    public string Team               { get; init; }

    // ── Kubernetes / host metadata ────────────────────────────────────────────
    public string HostName           { get; init; }
    public string K8sPodName         { get; init; }
    public string K8sNamespaceName   { get; init; }
    public string K8sNodeName        { get; init; }

    // ── Log transport ─────────────────────────────────────────────────────────
    public string LogLevel           { get; init; }
    public string LogType            { get; init; }
    public bool   EnableConsole      { get; init; }
    public bool   EnableFile         { get; init; }
    public string LogFilePath        { get; init; }
    public bool   EnableKafka        { get; init; }
    public string KafkaTopic         { get; init; }
    public List<string> KafkaBrokers { get; init; }

    // ── PII redaction ─────────────────────────────────────────────────────────
    public bool EnablePiiRedaction   { get; init; }

    private LogConfig() { }

    /// <summary>Creates a LogConfig by reading standard environment variables.</summary>
    public static LogConfig FromEnvironment(string serviceName)
    {
        return new LogConfig
        {
            ServiceName       = serviceName,
            ServiceVersion    = GetEnv("SERVICE_VERSION",    "unknown"),
            ServiceNamespace  = GetEnv("SERVICE_NAMESPACE",  "default"),
            Environment       = GetEnvMulti(["ENVIRONMENT", "ENV"], "production"),
            Team              = GetEnv("SERVICE_TEAM",        ""),
            HostName          = GetEnv("HOST_NAME",           System.Environment.MachineName),
            K8sPodName        = GetEnv("K8S_POD_NAME",        ""),
            K8sNamespaceName  = GetEnv("K8S_NAMESPACE_NAME",  ""),
            K8sNodeName       = GetEnv("K8S_NODE_NAME",       ""),
            LogLevel          = GetEnv("LOG_LEVEL",           "info"),
            LogType           = GetEnv("LOG_TYPE",            "app"),
            EnableConsole     = GetEnvBool("LOG_CONSOLE_ENABLED",      true),
            EnableFile        = GetEnvBool("LOG_FILE_ENABLED",         false),
            LogFilePath       = GetEnv("LOG_FILE_PATH",                "./logs/app.log"),
            EnableKafka       = GetEnvBool("LOG_KAFKA_ENABLED",        false),
            KafkaTopic        = GetEnv("KAFKA_LOG_TOPIC",              "logs.application"),
            KafkaBrokers      = ParseBrokers(GetEnv("KAFKA_BROKERS",   "")),
            EnablePiiRedaction = GetEnvBool("LOG_PII_REDACTION_ENABLED", true),
        };
    }

    public bool IsDevelopment => string.Equals(Environment, "development", StringComparison.OrdinalIgnoreCase);

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

    private static bool GetEnvBool(string key, bool defaultValue)
    {
        var raw = System.Environment.GetEnvironmentVariable(key);
        if (string.IsNullOrEmpty(raw)) return defaultValue;
        return raw.ToLowerInvariant() is "true" or "1" or "yes";
    }

    private static List<string> ParseBrokers(string raw) =>
        string.IsNullOrWhiteSpace(raw)
            ? []
            : [.. raw.Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries)];
}
