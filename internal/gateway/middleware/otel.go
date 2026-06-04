package middleware

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	iauth "github.com/inferbolthq/inferbolt/internal/auth"
)

const tracerName = "inferbolt/gateway"

type traceIDKey struct{}

// OTelTrace extracts incoming W3C TraceContext headers, starts a server span,
// and stores the trace ID in context so the logger can include it without
// importing the OTel SDK.
func OTelTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Propagate existing trace context from upstream (e.g. load balancer, browser).
		ctx := otel.GetTextMapPropagator().Extract(
			r.Context(),
			propagation.HeaderCarrier(r.Header),
		)

		tracer := otel.Tracer(tracerName)
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
				attribute.String("http.request_id", GetRequestID(ctx)),
			),
		)
		defer span.End()

		// Store trace ID as a plain string so the logger doesn't need an OTel import.
		if sc := span.SpanContext(); sc.IsValid() {
			ctx = context.WithValue(ctx, traceIDKey{}, sc.TraceID().String())
			w.Header().Set("X-Trace-ID", sc.TraceID().String())
		}

		// Tenant ID may be empty here (set by Auth later for protected routes).
		// The span attribute is set now and can be enriched downstream.
		if id, ok := iauth.GetTenantID(ctx); ok {
			span.SetAttributes(attribute.String("tenant_id", id))
		}

		rw := &wrappedWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.status_code", rw.status))
		if rw.status >= 500 {
			span.SetStatus(otelcodes.Error, http.StatusText(rw.status))
		}
	})
}

// GetTraceID returns the trace ID stored by OTelTrace, or "" if no span exists.
func GetTraceID(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey{}).(string)
	return id
}
