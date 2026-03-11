using System;
using System.Diagnostics;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Http;
using OpenTelemetry;

namespace Centricity.Observability.Tracing;

/// <summary>
/// ASP.NET Core middleware that creates a span for every HTTP request.
/// While OTel auto-instrumentation handles basic spans, this middleware adds
/// Centricity-specific attributes (service, team) and error recording.
///
/// Usage:
/// <code>
///   app.UseMiddleware&lt;HttpTracingMiddleware&gt;();
/// </code>
/// </summary>
public sealed class HttpTracingMiddleware
{
    private readonly RequestDelegate _next;
    private readonly string _serviceName;
    private readonly ActivitySource _source;

    public HttpTracingMiddleware(RequestDelegate next)
    {
        _next = next;
        _serviceName = Environment.GetEnvironmentVariable("SERVICE_NAME") ?? "unknown";
        _source = TracerFactory.GetTracer(_serviceName);
    }

    public async Task InvokeAsync(HttpContext context)
    {
        var method = context.Request.Method;
        var path = context.Request.Path.Value ?? "/";

        using var activity = _source.StartActivity($"{method} {path}");

        if (activity is not null)
        {
            // Add Centricity-specific attributes
            activity.SetTag("http.method", method);
            activity.SetTag("http.route", path);
            activity.SetTag("service", _serviceName);

            var team = Environment.GetEnvironmentVariable("SERVICE_TEAM");
            if (!string.IsNullOrEmpty(team))
                activity.SetTag("team", team);
        }

        try
        {
            await _next(context);

            if (activity is not null)
            {
                activity.SetTag("http.status_code", context.Response.StatusCode);
                if (context.Response.StatusCode >= 500)
                {
                    activity.SetStatus(ActivityStatusCode.Error,
                        $"HTTP {context.Response.StatusCode}");
                }
            }
        }
        catch (Exception ex)
        {
            activity?.SetStatus(ActivityStatusCode.Error, ex.Message);
            activity?.RecordException(ex);
            throw;
        }
    }
}
