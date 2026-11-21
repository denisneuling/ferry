// Package tracing sends ferry's traces over OTLP when the standard
// OTEL_EXPORTER_OTLP_ENDPOINT (or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) says
// where to; the exporter reads the other OTEL_* variables too. Without one,
// spans are made and dropped.
//
// Spans name routes and operations, never what they act on: no URL, path,
// share, file name or user name is an attribute (CONCEPT §6).
package tracing

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Name is the instrumentation scope of ferry's spans.
const Name = "github.com/metapatrol/ferry"

// Tracer is ferry's tracer, from the provider Init set — or the no-op one.
func Tracer() trace.Tracer { return otel.Tracer(Name) }

// Init sets up the exporter if one is configured, and returns what flushes
// and stops it.
func Init(ctx context.Context, version string) (func(), error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func() {}, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("tracing: %w", err)
	}
	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName("ferry"), semconv.ServiceVersion(version)))
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}, nil
}
