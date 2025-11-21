package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/sirupsen/logrus"
)

type Config struct {
	MaxRoutines        int
	RetryMax           int
	TargetURL          string
	Method             string
	RequestBody        string
	ExpectedStatusCode int
}

func getConfig() *Config {
	maxRetryStr, exists := os.LookupEnv("HTTP_RETRY_MAX")
	if !exists {
		logrus.Info("HTTP_RETRY_MAX not set, defaulting to 3")
		maxRetryStr = "3"
	}

	retryMax, _ := strconv.Atoi(maxRetryStr)

	maxroutinesStr, exists := os.LookupEnv("MAX_ROUTINES")
	if !exists {
		logrus.Info("MAX_ROUTINES not set, defaulting to 5")
		maxroutinesStr = "5"
	}
	maxRoutines, _ := strconv.Atoi(maxroutinesStr)

	targetURL, exists := os.LookupEnv("TARGET_URL")
	if !exists {
		err := fmt.Errorf("TARGET_URL not set, exiting")
		HandleError(err, true)
		return nil
	}

	method, exists := os.LookupEnv("HTTP_METHOD")
	if !exists {
		HandleError(fmt.Errorf("HTTP_METHOD not set, defaulting to GET"), false)
		method = "GET"
	}

	method = strings.ToUpper(method)
	allowedMethods := map[string]bool{
		"GET":    true,
		"POST":   true,
		"PUT":    true,
		"DELETE": true,
		"PATCH":  true,
	}

	if !allowedMethods[method] {
		err := fmt.Errorf("invalid HTTP_METHOD: %v, exiting", method)
		HandleError(err, true)
		return nil
	}

	body, exists := os.LookupEnv("REQUEST_BODY")
	if !exists {
		logrus.Info("REQUEST_BODY not set, defaulting to empty body")
	}

	return &Config{
		MaxRoutines: maxRoutines,
		RetryMax:    retryMax,
		TargetURL:   targetURL,
		Method:      method,
		RequestBody: body,
	}
}

func main() {
	config := getConfig()
	wg := sync.WaitGroup{}
	for i := 1; i <= config.MaxRoutines; i++ {
		logrus.Infof("Making request routine %v", i)
		wg.Add(1)
		go makeRequest(config, i, &wg)
	}

	wg.Wait()
	fmt.Println("All requests completed")
}

func makeRequest(c *Config, index int, wg *sync.WaitGroup) {
	//make request and log results; use some form of cancellation
	retryClient := getRetryClient(c)
	req, reqErr := retryablehttp.NewRequest(c.Method, c.TargetURL, bytes.NewBuffer([]byte(c.RequestBody)))
	if reqErr != nil {
		err := fmt.Errorf("error creating request: %v; index: %v", reqErr, index)
		HandleError(err, false)
		return
	}
	_, doErr := retryClient.Do(req)
	if doErr != nil {
		err := fmt.Errorf("error making request: %v; index %v", doErr, index)
		HandleError(err, false)
		return
	}

	logrus.Infof("Request %v completed successfully", index)
	wg.Done()
}

func getRetryClient(c *Config) *retryablehttp.Client {
	retryClient := retryablehttp.Client{
		HTTPClient: &http.Client{},
		RetryMax:   c.RetryMax,
		RequestLogHook: func(l retryablehttp.Logger, req *http.Request, count int) {
			logrus.Info("Making request to URL: %v, Retry Count: %v", req.URL, count)
		},
		ResponseLogHook: func(l retryablehttp.Logger, res *http.Response) {
			logrus.Info("URL: %v; Status: %v", res.Request.URL, res.StatusCode)
		},
	}
	return &retryClient
}

func HandleError(err error, fatal bool) {
	if err != nil {
		if fatal {
			logrus.Fatal()
		} else {
			logrus.Error(err)
		}
	}
}
