package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	_ "golang.org/x/oauth2"

	_ "github.com/cbdr/cb_go_oauth2"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/jhawk7/go-opentel/opentel"
	"github.com/pkg/errors"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

const MAX_ROUTINES int = 5
const EXPECTED_RESPONSE_CODE int = 200

var ds_meter metric.Meter

var SUCCESSES int = 0
var FAILURES int = 0

func main() {
	method := "GET"
	body := []byte(nil)
	url := "http://node5.local/isgomuxup"

	var wg sync.WaitGroup // create wait group (empty struct)

	if opentelErr := opentel.InitOpentelProviders(); opentelErr != nil {
		log.Fatal(opentelErr)
	}

	ds_meter = opentel.GetMeterProvider().Meter("deathstar2_meter")

	defer func() {
		shutdownErr := opentel.ShutdownOpentelProviders()
		ErrorHandler(shutdownErr, true)
	}()

	for i := 0; i < MAX_ROUTINES; i++ {
		wg.Add(1)
		go makeRequest(i, method, url, body, &wg)
	}

	wg.Wait()

	fmt.Printf("Successful calls: %v\n", SUCCESSES)
	fmt.Printf("Failed calls: %v\n", FAILURES)
}

func makeRequest(idx int, method string, url string, body []byte, wg *sync.WaitGroup) {
	//create trace from trace provider (tp set globally for otel lib)
	tr := opentel.GetTraceProvider().Tracer(fmt.Sprintf("request-%v", idx))
	ctx, span := tr.Start(context.Background(), fmt.Sprintf("api-call-%v", idx))
	span.SetAttributes(attribute.String("request.idx", fmt.Sprint(idx)))
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
	/*conf := &cb_go_oauth2.Config{
		ClientId: os.Getenv("CID"),
		Secret:   []byte(os.Getenv("SECRET")),
		TokenURL: os.Getenv("TOKEN_URL"),
	}*/

	/*httpClient := conf.Client(oauth2.NoContext)
	httpClient.Timeout = time.Duration(15 * time.Second)
	otelRoundTipper := otelhttp.NewTransport(httpClient.Transport)
	httpClient.Transport = otelRoundTipper*/

	retryClient := retryablehttp.NewClient()
	//wrapping retryclient roudtripper with otelhttp roundtripper
	retryClient.HTTPClient = &http.Client{Transport: otelhttp.NewTransport(retryClient.HTTPClient.Transport)}
	//retryClient.HTTPClient = httpClient
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
	} else {
		FAILURES++
	}
}

func ErrorHandler(err error, fatal bool) {
	if err != nil {
		fmt.Println(err)

		if fatal {
			panic(err)
		}
	}
}
