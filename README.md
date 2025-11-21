# go-deathStar2
![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=flat&logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/docker-%230db7ed.svg?style=flat&logo=docker&logoColor=white)

Go deathstar2 is a fun name for a stress tester that utilizes `go routines` to make a large number of requests to an API endpoint in parallel (to simulate stress).

## Environment Variables

The following environment variables can be set (see `setup_env.sh`):

| Variable         | Default      | Description                                      |
|------------------|-------------|--------------------------------------------------|
| HTTP_RETRY_MAX   | 3           | Maximum number of HTTP retries per request       |
| MAX_ROUTINES     | 100         | Maximum number of concurrent goroutines          |
| TARGET_URL       | (empty)     | Target API endpoint URL                          |
| HTTP_METHOD      | GET         | HTTP method to use for requests                  |
| REQUEST_BODY     | (empty)     | Request body (for POST/PUT methods)              |

To set these variables, you can source the `setup_env.sh` file:

```sh
source setup_env.sh
```


