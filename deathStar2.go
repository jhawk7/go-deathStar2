package main

import (
	"bytes"
	"context"
	"fmt"
	_ "io"
	"log"
	"net/http"
	"os"
	"sync"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	_ "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

const MAX_ROUTINES int = 5
const EXPECTED_RESPONSE_CODE int = 200
const tracerName string = "deathstar"

var outfile *os.File
var SUCCESSES int = 0

// returns a standard console exporter.
/*func newStdExporter(w io.Writer) (sdktrace.SpanExporter, error) {
	// Write telemetry data to a file.
	os.Remove("traces.txt")
	f, err := os.Create("traces.txt")
	if err != nil {
		log.Fatal(err)
	}
	outfile = f

	return stdout.New(
		stdout.WithWriter(w),
		// Use human-readable output.
		stdout.WithPrettyPrint(),
		// Do not print timestamps for the demo.
		stdout.WithoutTimestamps(),
	)
}*/

func initTraceProvider() *sdktrace.TracerProvider {
	//configure grpc exporter
	//exp_url := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") //should use localhost:4317 by default
	exporter, expErr := otlptracegrpc.New(context.Background() /*otlptracegrpc.WithEndpoint(exp_url)*/)
	if expErr != nil {
		//fmt.Errorf("error initializing exporter [error: %v]", expErr)
		log.Fatal(expErr)
	}

	//configure trace provider resource to describe this application
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(tracerName),
			semconv.ServiceVersionKey.String("v0.1.0"),
			attribute.String("environment", "development"),
		),
	)

	//register exporter with new trace provider
	tp := sdktrace.NewTracerProvider(
		//register exporter with trace provider using BatchSpanProcessor
		sdktrace.WithBatcher(exporter),
		//configure resource to be used in all traces from trace provider
		sdktrace.WithResource(r),
		//setup sampler to always sample traces
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	//register traceprovider as global tp
	otel.SetTracerProvider(tp)

	return tp
}

func getClient() *retryablehttp.Client {
	retryClient := retryablehttp.NewClient()
	//using otelhttp wrapped http client
	retryClient.HTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
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
	url := "http://localhost:8080/isgomuxup"

	var wg sync.WaitGroup // create wait group (empty struct)
	tp := initTraceProvider()
	//defer func to flush and stop trace provider and also close std outfile
	defer func() {
		if tpErr := tp.Shutdown(context.Background()); tpErr != nil {
			log.Fatal(tpErr)
		}

		if fileErr := outfile.Close(); fileErr != nil {
			log.Fatal(fileErr)
		}
	}()

	for i := 0; i < MAX_ROUTINES; i++ {
		wg.Add(1)
		go makeRequest(i, method, url, body, &wg)
	}

	wg.Wait()

	fmt.Printf("Successful calls: %v\n", SUCCESSES)
	fmt.Printf("Failed calls: %v\n", MAX_ROUTINES-SUCCESSES)
}

func makeRequest(idx int, method string, url string, body []byte, wg *sync.WaitGroup) {
	//create trace from trace provider (tp set globally for otel lib)
	tr := otel.Tracer(fmt.Sprintf("request-%v", idx))
	ctx, span := tr.Start(context.Background(), fmt.Sprintf("api-call-%v", idx))
	//span.SetAttributes(attribute.String("request.idx", fmt.Sprint(i)))
	defer span.End()

	//returned retryclient with otlphttp client
	retryClient := getClient()
	request, _ := retryablehttp.NewRequest(method, url, bytes.NewBuffer(body))
	//add ctx to request
	request = request.WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")

	span.AddEvent(fmt.Sprintf("making-request-%v", idx))
	response, requestErr := retryClient.Do(request)
	if requestErr != nil {
		err := errors.New(fmt.Sprintf("Error in request: %v\n", requestErr))
		span.AddEvent(fmt.Sprintf("Call failed; [URL: %v]", url))
		span.RecordError(err)
		fmt.Println(err)
	} else {
		span.AddEvent(fmt.Sprintf("Call made; [url: %v]", url))
		if response.StatusCode != EXPECTED_RESPONSE_CODE {
			span.SetStatus(codes.Error, response.Status)
		} else {
			span.SetStatus(codes.Ok, response.Status)
		}
	}
	wg.Done()
}
