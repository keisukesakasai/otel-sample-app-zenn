package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func initProvider() (func(context.Context) error, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("sample"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	conn, err := grpc.DialContext(ctx,
		"sample-collector.observability.svc.cluster.local:4317",
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())

	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)

	// otel.SetTextMapPropagator(propagation.TraceContext{})
	p := b3.New()
	otel.SetTextMapPropagator(p)

	return tracerProvider.Shutdown, nil
}

var tracer = otel.Tracer("sample")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	shutdown, err := initProvider()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			log.Fatal("failed to shutdown TracerProvider: %w", err)
		}
	}()

	r := gin.New()

	r.Use(
		otelgin.Middleware("sample"),
		requestid.New(
			requestid.WithCustomHeaderStrKey("x-request-id"),
		),
	)

	r.GET("/sample", sample1)

	r.Run(":8080")
}

func sample1(c *gin.Context) {
	_, span := tracer.Start(c.Request.Context(), "sample1 を実行")
	defer span.End()
	time.Sleep(time.Second * 1)
	sample2(c)
}

func sample2(c *gin.Context) {
	_, span := tracer.Start(c.Request.Context(), "sample2 を実行")
	defer span.End()
	time.Sleep(time.Second * 1)
	sample3(c)
}

func sample3(c *gin.Context) {
	_, span := tracer.Start(c.Request.Context(), "sample3 を実行")
	defer span.End()
	time.Sleep(time.Second * 1)

	msg := make(map[string]string)
	msg["x-request-id"] = requestid.Get(c)

	otel.GetTextMapPropagator().Inject(
		c.Request.Context(),
		propagation.MapCarrier(msg),
	)

	fmt.Println("id:" + requestid.Get(c))
}
