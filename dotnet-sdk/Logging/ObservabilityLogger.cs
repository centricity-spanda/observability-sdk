using Serilog;
using Serilog.Events;

namespace Centricity.Observability.Logging;

/// <summary>
/// Factory for creating a Serilog <see cref="ILogger"/> pre-configured with the
/// Centricity observability envelope format.
///
/// Usage:
/// <code>
///   var logger = ObservabilityLogger.Create("order-service");
///   logger.Information("Order created {@Attributes}", new { order_id = "O1", user_id = "U1" });
/// </code>
/// </summary>
public static class ObservabilityLogger
{
    /// <summary>
    /// Creates a Serilog logger using the <see cref="EnvelopeSink"/> for all outputs.
    /// Configuration is read from environment variables (same names as Go/Python SDKs).
    /// </summary>
    /// <param name="serviceName">The logical name of the service (e.g. "order-service").</param>
    /// <returns>A configured Serilog <see cref="ILogger"/>.</returns>
    public static ILogger Create(string serviceName)
    {
        var cfg    = LogConfig.FromEnvironment(serviceName);
        var sink   = new EnvelopeSink(cfg);
        var minLevel = ParseLevel(cfg.LogLevel);

        var logger = new LoggerConfiguration()
            .MinimumLevel.Is(minLevel)
            .WriteTo.Sink(sink)
            .CreateLogger();

        // Optionally set as the global Serilog instance
        Log.Logger = logger;

        return logger;
    }

    /// <summary>
    /// Creates a logger and assigns it as the global <see cref="Log.Logger"/>.
    /// Equivalent to <see cref="Create"/> but also returns the sink for disposal.
    /// </summary>
    public static (ILogger Logger, EnvelopeSink Sink) CreateWithSink(string serviceName)
    {
        var cfg  = LogConfig.FromEnvironment(serviceName);
        var sink = new EnvelopeSink(cfg);
        var minLevel = ParseLevel(cfg.LogLevel);

        var logger = new LoggerConfiguration()
            .MinimumLevel.Is(minLevel)
            .WriteTo.Sink(sink)
            .CreateLogger();

        Log.Logger = logger;
        return (logger, sink);
    }

    private static LogEventLevel ParseLevel(string level) => level.ToLowerInvariant() switch
    {
        "trace"   => LogEventLevel.Verbose,
        "debug"   => LogEventLevel.Debug,
        "info"    => LogEventLevel.Information,
        "warn"    => LogEventLevel.Warning,
        "warning" => LogEventLevel.Warning,
        "error"   => LogEventLevel.Error,
        "fatal"   => LogEventLevel.Fatal,
        _         => LogEventLevel.Information,
    };
}
