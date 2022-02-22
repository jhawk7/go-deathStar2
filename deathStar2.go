package main

import (
	"bytes"
	"context"
	"fmt"
	_ "io"
	"log"
	"net/http"
	_ "os"
	"sync"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/jhawk7/go-deathStar2/pkg/opentel"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
)

const MAX_ROUTINES int = 5
const EXPECTED_RESPONSE_CODE int = 200
const serviceName string = "deathstar"
var ds_meter metric.Meter
//var outfile *os.File
var SUCCESSES int = 0

func main() {
	method := "GET"
	body := []byte(nil)
	url := "http://node5.local/isgomuxup"

	var wg sync.WaitGroup // create wait group (empty struct)
	ctx := context.Background()

	//init global meter provider
	mp, mpErr := opentel.InitMeterProvider()
	if mpErr != nil {
		log.Fatal(mpErr)
	}

	//register meterProvider as global mp for package (meterProvider -> meter -> counter)
	global.SetMeterProvider(mp)

	//start metric collection
	if collectErr := mp.Start(ctx); collectErr != nil {
		log.Fatal(collectErr)
	}

	//create meter from meter provider (set to global variable)
	ds_meter = global.Meter("deathstar_meter")

	//init global trace provider
	tp, tpErr := opentel.InitTraceProvider()
	if tpErr != nil {
		log.Fatal(tpErr)
	}

	//register traceprovider as global tp for package (traceProvider -> trace -> span)
	otel.SetTracerProvider(tp)

	//defer func to flush and stop trace provider, close std outfile, stop meter provider collection
	defer func() {
		if shutdownErr := tp.Shutdown(ctx); shutdownErr != nil {
			log.Fatal(shutdownErr)
		}

		/* if fileErr := outfile.Close(); fileErr != nil {
			log.Fatal(fileErr)
		} */

		if stopErr := mp.Stop(ctx); stopErr != nil {
			log.Fatal(stopErr)
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

	//create counters for API call metrics
	successCtr, _ := ds_meter.NewInt64Counter("api.success_counter", metric.WithDescription("keeps count of successful API calls"))
	failCtr, _ := ds_meter.NewInt64Counter("api.fail_counter", metric.WithDescription("keeps count of failed API calls"))

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
			failCtr.Add(ctx, 1)
		} else {
			span.SetStatus(codes.Ok, response.Status)
			successCtr.Add(ctx, 1)
		}
	}
	wg.Done()
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
