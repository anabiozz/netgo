package netter

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

type Logger interface {
	Printf(string, ...interface{})
}

// Client represents http client
type Client struct {
	Inner *http.Client
	Logger
	Retry
}

// NewClient represents new http client
func NewClient() *Client {
	return defaultClient
}

var defaultClient = &Client{
	Inner: &http.Client{
		Timeout:   30 * time.Second,
		Transport: defaultTransport,
	},
	Logger: log.New(os.Stderr, "", log.LstdFlags),
	Retry: Retry{
		WaitMin: 2 * time.Second,
		WaitMax: 8 * time.Second,
		Max:     4,
	},
}

// Do sends an HTTP request and returns an HTTP response
func (c *Client) Do(req *Request) (resp *http.Response, err error) {

	for i := 0; ; i++ {

		var code int

		if req.body != nil {
			body, err := req.body()
			if err != nil {
				return resp, err
			}
			if c, ok := body.(io.ReadCloser); ok {
				req.Body = c
			} else {
				req.Body = ioutil.NopCloser(body)
			}
		}

		resp, err = c.Inner.Do(req.Request)
		if resp != nil {
			code = resp.StatusCode
		}
		if err != nil {
			c.Logger.Printf("netter: %s request failed: %v", req.URL, err)
		}

		retryable, checkErr := c.Retry.isRetry(req.Context(), resp, err)

		if !retryable {
			if checkErr != nil {
				err = checkErr
			}
			return resp, err
		}

		remain := c.Retry.Max - i
		if remain <= 0 {
			break
		}

		if err == nil && resp != nil {
			c.drainBody(resp.Body)
		}

		wait := c.Retry.backoff(c.Retry.WaitMin, c.Retry.WaitMax, i)

		desc := fmt.Sprintf("%s (status: %d)", req.URL, code)
		c.Logger.Printf("netter: %s retrying in %s (%d left)", desc, wait, remain)

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}

	if resp != nil {
		if err := resp.Body.Close(); err != nil {
			c.Logger.Printf(err.Error())
		}
	}
	return nil, fmt.Errorf("netter: %s giving up after %d attempts", req.URL, c.Max+1)
}

func (c *Client) drainBody(body io.ReadCloser) {
	_, err := io.Copy(ioutil.Discard, io.LimitReader(body, 4096))
	if err != nil {
		c.Logger.Printf("netter: reading response body: %v", err)
	}

	err = body.Close()
	if err != nil {
		c.Logger.Printf(err.Error())
	}
}

// Get sends get request
func Get(url string) (*http.Response, error) {
	return defaultClient.Get(url)
}

// Get sends get request
func (c *Client) Get(url string) (*http.Response, error) {
	req, err := NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// Post sends post request
func Post(url, bodyType string, body interface{}) (*http.Response, error) {
	return defaultClient.Post(url, bodyType, body)
}

// Post sends post request
func (c *Client) Post(url, bodyType string, body interface{}) (*http.Response, error) {
	req, err := NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", bodyType)
	return c.Do(req)
}
