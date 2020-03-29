package netgo

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var robotsTxtHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Last-Modified", "sometime")
	_, err := fmt.Fprintf(w, "User-agent: go\nDisallow: /something/")
	if err != nil {
		fmt.Println(err)
	}
})

func TestClient(t *testing.T) {
	setParallel(t)
	defer afterTest(t)

	ts := httptest.NewServer(robotsTxtHandler)
	defer ts.Close()

	client := NewClient()
	client.Max = 4
	client.WaitMin = 2 * time.Second
	client.WaitMax = 8 * time.Second

	res, err := client.Get(ts.URL)
	var bytes []byte

	if err == nil {
		bytes, err = pedanticReadAll(res.Body)
		err := res.Body.Close()
		if err != nil {
			t.Error(err)
		}
	}
	if err != nil {
		t.Error(err)
	} else if s := string(bytes); !strings.HasPrefix(s, "User-agent:") {
		t.Errorf("incorrect page body (did not begin with User-agent: %q", s)
	}
}

func pedanticReadAll(r io.Reader) (b []byte, err error) {
	var buffer [64]byte
	buf := buffer[:]
	for {
		n, err := r.Read(buf)
		if n == 0 && err == nil {
			return nil, fmt.Errorf("read: n=0 with err=nil")
		}
		b = append(b, buf[:n]...)
		if err == io.EOF {
			n, err := r.Read(buf)
			if n != 0 || err != io.EOF {
				return nil, fmt.Errorf("read: n=%d err=%#v after EOF", n, err)
			}
			return b, nil
		}
		if err != nil {
			return b, nil
		}
	}
}

func interestingGoroutines() (gs []string) {
	buf := make([]byte, 2<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for _, g := range strings.Split(string(buf), "\n\n") {
		sl := strings.SplitN(g, "\n", 2)
		if len(sl) != 2 {
			continue
		}
		stack := strings.TrimSpace(sl[1])
		if stack == "" ||
			strings.Contains(stack, "testing.(*M).before.func1") ||
			strings.Contains(stack, "os/signal.signal_recv") ||
			strings.Contains(stack, "created by net.startServer") ||
			strings.Contains(stack, "created by testing.RunTests") ||
			strings.Contains(stack, "closeWriteAndWait") ||
			strings.Contains(stack, "testing.Main(") ||
			strings.Contains(stack, "runtime.goexit") ||
			strings.Contains(stack, "created by runtime.gc") ||
			strings.Contains(stack, "net/http_test.interestingGoroutines") ||
			strings.Contains(stack, "runtime.MHeap_Scavenger") {
			continue
		}
		gs = append(gs, stack)
	}
	sort.Strings(gs)
	return
}

func setParallel(t *testing.T) {
	if testing.Short() {
		t.Parallel()
	}
}

func afterTest(t testing.TB) {
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()
	if testing.Short() {
		return
	}
	var bad string

	badSubstring := map[string]string{
		").readLoop(":  "a Transport",
		").writeLoop(": "a Transport",
		"created by net/http/httptest.(*Server).Start": "an httptest.Server",
		"timeoutHandler":        "a TimeoutHandler",
		"net.(*netFD).connect(": "a timing out dial",
		").noteClientGone(":     "a closenotifier sender",
	}
	var stacks string
	for i := 0; i < 4; i++ {
		bad = ""
		stacks = strings.Join(interestingGoroutines(), "\n\n")
		for substr, what := range badSubstring {
			if strings.Contains(stacks, substr) {
				bad = what
			}
		}
		if bad == "" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("test appears to have leaked %s:\n%s", bad, stacks)
}

type clientServerTest struct {
	t  *testing.T
	h2 bool
	h  http.Handler
	ts *httptest.Server
	tr *http.Transport
	c  *Client
}

func (t *clientServerTest) close() {
	t.tr.CloseIdleConnections()
	t.ts.Close()
}

func newClientServerTest(t *testing.T, tr *http.Transport, h http.Handler, opts ...interface{}) *clientServerTest {
	cst := &clientServerTest{
		t:  t,
		h:  h,
		tr: tr,
	}
	cst.c = &Client{Inner: &http.Client{Transport: cst.tr}}
	cst.ts = httptest.NewUnstartedServer(h)

	for _, opt := range opts {
		switch opt := opt.(type) {
		case func(*http.Transport):
			opt(cst.tr)
		case func(*httptest.Server):
			opt(cst.ts)
		default:
			t.Fatalf("unhandled option type %T", opt)
		}
	}

	cst.ts.Start()

	return cst
}

func TestClientHead(t *testing.T) {
	cst := newClientServerTest(t, defaultTransport, robotsTxtHandler)
	defer cst.close()

	r, err := cst.c.Inner.Head(cst.ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Header["Last-Modified"]; !ok {
		t.Error("Last-Modified header not found.")
	}
}

type countHandler struct {
	mu sync.Mutex
	n  int
}

const (
	maxAttemptRetry = 5
)

func (h *countHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	if h.n == maxAttemptRetry {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(strconv.Itoa(h.n)))
		if err != nil {
			fmt.Print(err)
		}
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func TestClientRetry(t *testing.T) {
	setParallel(t)
	defer afterTest(t)

	ts := httptest.NewServer(new(countHandler))
	defer ts.Close()

	client := NewClient()
	client.Max = maxAttemptRetry
	client.WaitMin = 2 * time.Second
	client.WaitMax = 8 * time.Second

	res, err := client.Get(ts.URL)
	if err != nil {
		t.Error(err)
	}

	var bytes []byte
	var counter int

	bytes, err = pedanticReadAll(res.Body)
	err = res.Body.Close()
	if err != nil {
		t.Error(err)
	}

	if string(bytes) != "" {
		counter, err = strconv.Atoi(string(bytes))
		if err != nil {
			t.Error(err)
		}
	}

	if counter != maxAttemptRetry {
		t.Errorf("counter should be %d", maxAttemptRetry)
	}
}

func TestClientRetryFail(t *testing.T) {
	setParallel(t)
	defer afterTest(t)

	ts := httptest.NewServer(new(countHandler))
	defer ts.Close()

	client := NewClient()
	client.Max = maxAttemptRetry - 2
	client.WaitMin = 2 * time.Second
	client.WaitMax = 8 * time.Second

	res, err := client.Get(ts.URL)
	if err != nil {
		if !strings.Contains(err.Error(), "giving up after 4 attempts") {
			t.Error("error should be 'giving up after 4 attempts'")
		}
	} else {
		t.Error("should be error")
		err = res.Body.Close()
		if err != nil {
			t.Error(err)
		}
	}
}

var timeoutDefaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		// Limits the time spent establishing a TCP connection
		// Errors:
		// i/o timeout
		Timeout: 30 * time.Millisecond,
		// TCP KeepAlive specifies the interval between keep-alive probes for an active network connection.
		KeepAlive: 30 * time.Millisecond,
	}).DialContext,
	// Limits the time spent reading the headers of the response
	// Errors:
	// net/http: timeout awaiting response headers
	ResponseHeaderTimeout: 600 * time.Millisecond,
	MaxIdleConns:          100,
	// How long an idle connection is kept in the connection pool
	IdleConnTimeout:       90 * time.Millisecond,
	ExpectContinueTimeout: 5 * time.Millisecond,
	DisableKeepAlives:     true,
	MaxIdleConnsPerHost:   -1,
}

func TestClientTimeout(t *testing.T) {
	setParallel(t)
	defer afterTest(t)
	testDone := make(chan struct{})

	sawRoot := make(chan bool, 1)
	sawSlow := make(chan bool, 1)

	cst := newClientServerTest(t, timeoutDefaultTransport, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			sawRoot <- true
			time.Sleep(200 * time.Millisecond)
			http.Redirect(w, req, "/slow", http.StatusFound)
			return
		}
		if req.URL.Path == "/slow" {
			sawSlow <- true
			if _, err := w.Write([]byte("Hello")); err != nil {
				t.Error(err)
				return
			}
			w.(http.Flusher).Flush()
			<-testDone
			return
		}
	}))
	defer cst.close()
	defer close(testDone)

	const timeout = 200 * time.Millisecond
	cst.c.Inner.Timeout = timeout

	res, err := cst.c.Inner.Get(cst.ts.URL)
	if err != nil {
		t.Log(err)
		if strings.Contains(err.Error(), "Client.Timeout") {
			t.Skipf("host too slow to get fast resource in %v", timeout)
		}
		t.Fatal(err)
	}

	select {
	case <-sawRoot:
	default:
		t.Fatal("handler never got / request")
	}

	select {
	case <-sawSlow:
	default:
		t.Fatal("handler never got /slow request")
	}

	errc := make(chan error, 1)
	go func() {
		_, err := ioutil.ReadAll(res.Body)
		errc <- err
		if err := res.Body.Close(); err != nil {
			t.Error(err)
		}
	}()

	const failTime = 5 * time.Second
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("expected error from ReadAll")
		}
		ne, ok := err.(net.Error)
		if !ok {
			t.Errorf("error value from ReadAll was %T; expected some net.Error", err)
		} else if !ne.Timeout() {
			t.Errorf("net.Error.Timeout = false; want true")
		}
		if got := ne.Error(); !strings.Contains(got, "Client.Timeout exceeded") {
			t.Errorf("error string = %q; missing timeout substring", got)
		}
	case <-time.After(failTime):
		t.Errorf("timeout after %v waiting for timeout of %v", failTime, timeout)
	}
}
