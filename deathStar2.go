package main

import (
	"bytes"
	"fmt"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"net/http"
	"github.com/pkg/errors"
	"sync"
)

const MAX_ROUTINES int = 10000

func main() {
	method := "GET"
	body := []byte(nil)
	url := "http://localhost:8080/healthcheck"

	var wg sync.WaitGroup // create wait group (empty struct)

	for i := 0; i < MAX_ROUTINES; i++ {
		wg.Add(1)
		go makeRequest(method, url, body, &wg)
	}

	wg.Wait()
}

var RequestHook = func(logger retryablehttp.Logger, req *http.Request, retryNumber int) {
	fmt.Printf("Making Request to URL: %s; Retry Count: %v\n", req.URL, retryNumber)
}

var ResponseHook = func(logger retryablehttp.Logger, res *http.Response) {
	fmt.Printf("URL: %v responded with Status: %v\n", res.Request.URL, res.StatusCode)
}

func makeRequest(method string, url string, body []byte, wg *sync.WaitGroup) {
	retryClient := getClient()

	request, _ := retryablehttp.NewRequest(method, url, bytes.NewBuffer(body))
	request.Header.Set("Content-Type", "application/json")
	response, requestErr := retryClient.Do(request)
	if requestErr != nil {
		err := errors.New(fmt.Sprintf("Error in request: %v\n", requestErr))
		fmt.Println(err)
		return
	}

	fmt.Println(response.StatusCode)
	wg.Done()
}

func getClient() *retryablehttp.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.RequestLogHook = RequestHook
	retryClient.ResponseLogHook = ResponseHook
	retryClient.RetryMax = 3

	return retryClient
}
