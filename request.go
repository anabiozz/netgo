package netter

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
)

// ReaderFunc represents request body type
type ReaderFunc func() (io.Reader, error)

// Request ..
type Request struct {
	body ReaderFunc
	*http.Request
}

type lenner interface {
	Len() int
}

// NewRequest ..
func NewRequest(method, url string, rawBody interface{}) (*Request, error) {
	bodyReader, contentLength, err := getBodyReader(rawBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.ContentLength = contentLength

	return &Request{bodyReader, httpReq}, nil
}

func getBodyReader(body interface{}) (bodyReader ReaderFunc, contentLength int64, err error) {
	if body != nil {
		switch bodyType := body.(type) {
		case ReaderFunc:
			bodyReader = bodyType
			tmp, err := bodyType()
			if err != nil {
				return nil, 0, err
			}
			if lr, ok := tmp.(lenner); ok {
				contentLength = int64(lr.Len())
			}
			if c, ok := tmp.(io.Closer); ok {
				err := c.Close()
				if err != nil {
					return nil, 0, err
				}
			}

		case *bytes.Reader:
			bodyReader = func() (io.Reader, error) {
				return ioutil.NopCloser(bodyType), nil
			}
			contentLength = int64(bodyType.Len())

		case *bytes.Buffer:
			buf := bodyType.Bytes()
			bodyReader = func() (io.Reader, error) {
				r := bytes.NewReader(buf)
				return ioutil.NopCloser(r), nil
			}
			contentLength = int64(bodyType.Len())

		case *strings.Reader:
			bodyReader = func() (io.Reader, error) {
				return ioutil.NopCloser(bodyType), nil
			}
			contentLength = int64(bodyType.Len())

		case io.Reader:
			buf, err := ioutil.ReadAll(bodyType)
			if err != nil {
				return nil, 0, err
			}
			bodyReader = func() (io.Reader, error) {
				return ioutil.NopCloser(bytes.NewReader(buf)), nil
			}
			contentLength = int64(len(buf))

		default:
			return nil, 0, fmt.Errorf("cannot handle type %T", bodyType)
		}
	}
	return bodyReader, contentLength, nil
}
