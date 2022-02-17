package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

const MAX_ROUTINES int = 5
const EXPECTED_RESPONSE_CODE int = 200
const tracerName string = "deathstar"

var SUCCESSES int = 0

func initTraceProvider() *sdktrace.TracerProvider {
	//configure exporter
	exp_url := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	exporter, expErr := otlptrace.New(context.Background(), otlptracegrpc.NewClient(otlptracegrpc.WithEndpoint(exp_url)))
	if expErr != nil {
		fmt.Errorf("error initializing exporter [error: %v]", expErr)
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

	var wg sync.WaitGroup       // create wait group (empty struct)
	ctx := context.Background() //empty context to be used for trace
	tp := initTraceProvider()
	//defer func to flush and stop trace provider and also close std outfile
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	//init main span tracer
	//spanCtx, span := tp.Tracer(tracerName).Start(ctx, "main")
	//defer span.End()

	for i := 0; i < MAX_ROUTINES; i++ {
		wg.Add(1)
		//_, span := otel.Tracer(tracerName).Start(spanCtx, "makeRequest")
		//_, span := tp.Tracer(tracerName).Start(spanCtx, fmt.Sprintf("makeRequest-%v", i))
		//defer span.End()
		//span.SetAttributes(attribute.String("request.idx", fmt.Sprint(i)))
		go makeRequest(i, method, url, body, &wg)
	}

	wg.Wait()

	fmt.Printf("Successful calls: %v\n", SUCCESSES)
	fmt.Printf("Failed calls: %v\n", MAX_ROUTINES-SUCCESSES)
}

func makeRequest(idx int, method string, url string, body []byte, wg *sync.WaitGroup) {
	//_, span := otel.Tracer(tracerName).Start(*ctx, "makeRequest")
	//spanCtx, span := tracer.Start(*ctx, "makeRequest")
	//create trace from trace provider (tp set for otel lib)
	tr := otel.Tracer(fmt.Sprintf("request-", idx))
	ctx, span := tr.Start(context.Background(), "api call")
	defer span.End()

	//returned retryclient with otlphttp client
	retryClient := getClient()
	request, _ := retryablehttp.NewRequest(method, url, bytes.NewBuffer(body))
	//add ctx to request
	request = request.WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")

	span.AddEvent("making-request")
	response, requestErr := retryClient.Do(request)
	if requestErr != nil {
		err := errors.New(fmt.Sprintf("Error in request: %v\n", requestErr))
		//span.AddEvent(fmt.Sprintf("Request failed. [url: %v] [status: %v]", url, response.StatusCode))
		//span.RecordError(err)
		fmt.Println(err)
	} else {
		//span.AddEvent(fmt.Sprintf("Request succeeded. [url: %v] [status: %v]", url, response.Status))
		fmt.Println(response.StatusCode)
	}
	wg.Done()
}
