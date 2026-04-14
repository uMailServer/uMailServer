# Distributed Tracing

uMailServer supports distributed tracing using OpenTelemetry, enabling you to monitor and debug requests across SMTP, IMAP, POP3, HTTP, and internal components.

## Overview

Distributed tracing provides visibility into:
- Request flow through the email pipeline (SMTP → Queue → IMAP)
- Latency breakdown by component
- Error tracking and context propagation
- Performance bottlenecks

## Configuration

Add the `tracing` section to your `umailserver.yaml`:

```yaml
# Distributed Tracing (OpenTelemetry)
tracing:
  enabled: false               # Enable OpenTelemetry tracing
  service_name: "umailserver"  # Service name shown in traces
  exporter: "noop"            # Exporter: "noop", "stdout", or "otlp"
  otlp_endpoint: "localhost:4317"  # OTLP collector endpoint (for otlp exporter)
  environment: "production"   # Environment tag: "production", "staging", "development"
  sample_rate: 1.0            # Sampling rate 0.0-1.0 (1.0 = trace all requests)
  attributes:                 # Additional resource attributes
    deployment: "primary"
    region: "us-east-1"
```

## Exporters

### Noop (Default)
Discards all traces. Use this when tracing is not needed.

```yaml
tracing:
  enabled: true
  exporter: "noop"
```

### Stdout
Outputs traces to stdout in JSON format. Useful for debugging.

```yaml
tracing:
  enabled: true
  exporter: "stdout"
```

### OTLP
Sends traces to an OTLP-compatible collector (e.g., Jaeger, Tempo, or OpenTelemetry Collector).

```yaml
tracing:
  enabled: true
  exporter: "otlp"
  otlp_endpoint: "localhost:4317"
```

## Sampling

Control the volume of traces with sampling:

```yaml
tracing:
  enabled: true
  exporter: "otlp"
  sample_rate: 0.1  # Trace 10% of requests
```

## Integration with Jaeger

1. Start Jaeger:
```bash
docker run -d --name jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

2. Configure uMailServer:
```yaml
tracing:
  enabled: true
  exporter: "otlp"
  otlp_endpoint: "localhost:4317"
  service_name: "umailserver"
  environment: "production"
```

3. View traces at http://localhost:16686

## Span Attributes

The following attributes are automatically added to spans:

- `service.name` - Service name from configuration
- `service.version` - uMailServer version
- `deployment.environment` - Environment (production/staging/development)
- Custom attributes from configuration

## Propagation

Traces are propagated across:
- HTTP requests (via `traceparent` header)
- Internal component boundaries
- SMTP/IMAP/POP3 sessions

## Performance Impact

Tracing has minimal overhead:
- Noop exporter: ~0 overhead
- Stdout exporter: <1% overhead
- OTLP exporter: <5% overhead (async batching)

## Troubleshooting

### No traces appearing

1. Verify tracing is enabled:
```yaml
tracing:
  enabled: true
```

2. Check exporter configuration:
```yaml
tracing:
  exporter: "otlp"
  otlp_endpoint: "localhost:4317"
```

3. Verify OTLP collector is reachable:
```bash
telnet localhost 4317
```

### High memory usage

Reduce sampling rate:
```yaml
tracing:
  sample_rate: 0.01  # Trace 1% of requests
```

## See Also

- [OpenTelemetry Documentation](https://opentelemetry.io/docs/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Production Readiness Guide](./PRODUCTION_READINESS.md)
