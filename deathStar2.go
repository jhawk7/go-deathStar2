package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	trace "go.opentelemetry.io/otel/trace"
)

const MAX_ROUTINES int = 5
const EXPECTED_RESPONSE_CODE int = 200
const tracerName string = "deathstar"

var COLLECTER_URL string = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

//var tp *sdktrace.TracerProvider
var otp OtelProvider

type OtelProvider struct {
	provider *sdktrace.TracerProvider
	outfile  *os.File
}

//const tracer = global.Tracer("deathstar")

var SUCCESSES int = 0

// newExporter returns a console exporter.
func newStdExporter(w io.Writer) (sdktrace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(w),
		// Use human-readable output.
		stdouttrace.WithPrettyPrint(),
		// Do not print timestamps for the demo.
		stdouttrace.WithoutTimestamps(),
	)
}

func newHttpExporter() (exporter *otlptrace.Exporter, err error) {
	exporter, err = otlptrace.New(context.Background(), otlptracehttp.NewClient(otlptracehttp.WithEndpoint(COLLECTER_URL)))
	if err != nil {
		fmt.Errorf("error initializing exporter [error: %v]", err)
		//log.Fatal(err)
		return
	}

	return
}

func newGRPCExporter() (exporter *otlptrace.Exporter, err error) {
	exporter, err = otlptrace.New(context.Background(), otlptracegrpc.NewClient(otlptracegrpc.WithEndpoint(COLLECTER_URL)))
	if err != nil {
		fmt.Errorf("error initializing exporter [error: %v]", err)
		//log.Fatal(err)
		return
	}

	return
}

// newResource returns a resource describing this application.
func newResource() *resource.Resource {
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(tracerName),
			semconv.ServiceVersionKey.String("v0.1.0"),
			attribute.String("environment", "demo"),
		),
	)
	return r
}

func initTraceProvider() {
	l := log.New(os.Stdout, "", 0)

	// Write telemetry data to a file.
	f, err := os.Create("traces.txt")
	if err != nil {
		l.Fatal(err)
	}
	//defer f.Close()

	//create console exporter to export to file
	exp, err := newStdExporter(f)
	if err != nil {
		l.Fatal(err)
	}

	/* exp, err := newGRPCExporter()
	if err != nil {
		log.Fatal(err)
	}
	*/
	//register exporter with new trace provider
	tp := sdktrace.NewTracerProvider(
		//sdktrace.WithBatcher(exp),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exp)),
		sdktrace.WithSyncer(exp),
		sdktrace.WithResource(newResource()),
	)

	/* //defer func to flush and stop trace provider
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			l.Fatal(err)
		}
	}() */

	//register traceprovider as global tp
	otel.SetTracerProvider(tp)

	otp.provider = tp
	otp.outfile = f
}

func getClient() *retryablehttp.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RequestLogHook = RequestHook
	retryClient.ResponseLogHook = ResponseHook
	retryClient.RetryMax = 3

	return retryClient
}

var RequestHook = func(logger retryablehttp.Logger, req *http.Request, retryNumber int) {
	fmt.Printf("Making Request to URL: %s; Retry Count: %v\n", req.URL, retryNumber)
}

var ResponseHook = func(logger retryablehttp.Logger, res *http.Response) {
	fmt.Printf("URL: %v responded with Status: %v\n", res.Request.URL, res.StatusCode)
	if res.StatusCode == EXPECTED_RESPONSE_CODE {
		SUCCESSES++
	}
}

func main() {
	//method := "POST"
	//body := []byte(`{"dsar_request_id":"dr0525f0b8-f494-47a3-b495-13c7b407ff06"}`)
	//url := "http://localhost:8080/dsar/reflow"
	method := "GET"
	body := []byte(nil)
	//url := "http://localhost:8001/api/v1/namespaces/default/services/rpi-gomux-service:80/proxy/isgomuxup"
	url := "http://localhost:8000/isgomuxup"

	var wg sync.WaitGroup       // create wait group (empty struct)
	ctx := context.Background() //empty context to be used for trace
	initTraceProvider()

	//defer func to flush and stop trace provider and also close std outfile
	defer func() {
		if err := otp.provider.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}

		if fileErr := otp.outfile.Close(); fileErr != nil {
			log.Fatal(fileErr)
		}
	}()

	//init main span tracer
	spanCtx, span := otp.provider.Tracer(tracerName).Start(ctx, "main")
	defer span.End()

	for i := 0; i < MAX_ROUTINES; i++ {
		wg.Add(1)
		//_, span := otel.Tracer(tracerName).Start(spanCtx, "makeRequest")
		_, span := otp.provider.Tracer(tracerName).Start(spanCtx, fmt.Sprintf("makeRequest-%v", i))
		//defer span.End()
		span.SetAttributes(attribute.String("request.idx", fmt.Sprint(i)))
		go makeRequest(span, method, url, body, &wg)
	}

	wg.Wait()

	fmt.Printf("Successful calls: %v\n", SUCCESSES)
	fmt.Printf("Failed calls: %v\n", MAX_ROUTINES-SUCCESSES)
}

func makeRequest(span trace.Span, method string, url string, body []byte, wg *sync.WaitGroup) {
	//_, span := otel.Tracer(tracerName).Start(*ctx, "makeRequest")
	//spanCtx, span := tracer.Start(*ctx, "makeRequest")
	defer span.End()
	retryClient := getClient()
	//span.SetAttributes(attribute.String("request.n"))

	request, _ := retryablehttp.NewRequest(method, url, bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	span.AddEvent("making-request")
	response, requestErr := retryClient.Do(request)
	if requestErr != nil {
		err := errors.New(fmt.Sprintf("Error in request: %v\n", requestErr))
		span.AddEvent(fmt.Sprintf("Request failed. [url: %v] [status: %v]", url, response.StatusCode))
		span.RecordError(err)
		fmt.Println(err)
	} else {
		span.AddEvent(fmt.Sprintf("Request succeeded. [url: %v] [status: %v]", url, response.Status))
		fmt.Println(response.StatusCode)
	}
	wg.Done()
}
