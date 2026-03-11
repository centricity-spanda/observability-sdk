using System;
using System.Diagnostics;
using System.Diagnostics.Metrics;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Http;

namespace Centricity.Observability.Metrics;

/// <summary>
/// ASP.NET Core middleware that automatically records RED metrics for every HTTP request:
///   • http_requests_total        (counter)     [service, method, path, status]
///   • http_request_duration_seconds (histogram) [service, method, path]
///   • http_requests_in_flight    (gauge)       [service]
///
/// Usage:
/// <code>
///   app.UseMiddleware&lt;HttpMetricsMiddleware&gt;();
/// </code>
/// </summary>
public sealed class HttpMetricsMiddleware
{
    private readonly RequestDelegate _next;
    private readonly string _serviceName;

    public HttpMetricsMiddleware(RequestDelegate next)
    {
        _next = next;
        _serviceName = Environment.GetEnvironmentVariable("SERVICE_NAME") ?? "unknown";
    }

    public async Task InvokeAsync(HttpContext context)
    {
        var method = context.Request.Method;
        var path = NormalizePath(context.Request.Path.Value ?? "/");

        // Track in-flight
        MetricsFactory.OnHttpRequestStart(_serviceName);

        var sw = Stopwatch.StartNew();
        try
        {
            await _next(context);
        }
        finally
        {
            sw.Stop();
            var statusCode = context.Response.StatusCode;
            var duration = sw.Elapsed.TotalSeconds;

            MetricsFactory.RecordHttpRequest(
                _serviceName, method, path, statusCode, duration);
        }
    }

    /// <summary>
    /// Normalizes request paths to prevent cardinality explosion.
    /// Replaces numeric/GUID segments with placeholders.
    /// </summary>
    private static string NormalizePath(string path)
    {
        // Replace GUID segments
        path = System.Text.RegularExpressions.Regex.Replace(
            path,
            @"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
            ":id");

        // Replace pure numeric segments like /orders/12345 → /orders/:id
        path = System.Text.RegularExpressions.Regex.Replace(
            path,
            @"/\d+",
            "/:id");

        // Truncate very long paths
        if (path.Length > 50)
            path = path[..50] + "...";

        return path;
    }
}
