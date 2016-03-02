package main

import (
	"bytes"
	"flag"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"time"
	"io/ioutil"
	"math/rand"
	"strings"
	log "github.com/Sirupsen/logrus"
	"fmt"
	"sync/atomic"
	"sync"
)

func LogDebugWithTime(s string, r string) {
	log.WithFields(log.Fields{
		"Timestamp": time.Now(),
		"request_id": r,
	}).Debug(s)
}

func LogErrorWithTime(s string, r string) {
	log.WithFields(log.Fields{
		"Timestamp": time.Now(),
		"request_id": r,
	}).Error(s)
}


// Console flags
var (
	listen = flag.String("l", ":8888", "port to accept requests")
	targetProduction = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	alternateTimeout = flag.Int("b.timeout", 3, "timeout in seconds for alternate site traffic")
	workers = flag.Int("w", 1, "number of workers allowed to send traffic to b")
)

var tasks = make(chan request_task, 128)

var ops uint64 = 0

// handler contains the address of the main Target and the one for the Alternative target
type handler struct {
	Target      string
	Alternative string
}

type request_task struct {
	Request http.Request
	ID      string
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error {
	return nil
}

// Be sure we have a scheme in our host URIs, to accomodate the rather crappy go-native tokenization
func LocalParseURL(rawurl string) (u *url.URL, err error) {
	if !strings.Contains(rawurl, "http://") && !strings.Contains(rawurl, "https://") {
		rawurl = "http://" + rawurl
	}
	return url.Parse(rawurl)
}

// Generate working request_ids for debug
func RandomString(strlen int) string {
	rand.Seed(time.Now().UTC().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func MakeRequestID() (id string) {
	return RandomString(32)
}

// channelized worker queue for dispatching mirrored requests
func worker(tasks <-chan request_task, quit <-chan bool, tick <-chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case task, ok := <-tasks:
			if !ok {
				return
			}

			// create and dispatch asynch. request to alternate
			alt_target_parse, err := LocalParseURL(*altTarget)
			if err != nil {
				LogErrorWithTime(fmt.Sprintf("Failed to parse Target: %s: %v\n", *altTarget, err), task.ID)
			}
			task.Request.URL.Host = alt_target_parse.Host
			task.Request.URL.Scheme = alt_target_parse.Scheme

			LogDebugWithTime("Bulding Alternate Request", task.ID)
			clientHttpConn1 := &http.Client{
				Timeout: time.Duration(time.Duration(*alternateTimeout) * time.Second),
			}

			_, err = clientHttpConn1.Do(&task.Request)
			if err != nil {
				LogErrorWithTime(fmt.Sprintf("Failed to send to %s: %v\n", task.Request.Host, err), task.ID)
				return
			}
			LogDebugWithTime("Altnernate Request Finished", task.ID)
		case <-quit:
			return
		case <-tick:
			if len(tasks) <= 64{
				log.WithFields(log.Fields{
					"Tick": time.Now(),
					"Buffered Jobs": len(tasks),
				}).Debug("Status")
			} else {
				log.WithFields(log.Fields{
					"Tick": time.Now(),
					"Buffered Jobs": len(tasks),
				}).Warn("Status")
			}

		}
	}
}

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// process accounting
	thisop := atomic.LoadUint64(&ops)
	atomic.AddUint64(&ops, 1)
	requestID := MakeRequestID()

	// preliminary logging
	LogDebugWithTime(fmt.Sprintf("New Request Op: %d", thisop), requestID)
	log.WithFields(
		log.Fields{
			"request_op": thisop,
			"request_id": requestID,
			"request_method": req.Method,
			"request_path": req.URL.RequestURI(),
		}).Debug("Incomming Request")

	// cppy the request
	alt_request, target_request := DuplicateRequest(req)

	tasks <- request_task {
		Request:           *alt_request,
		ID:                requestID,
	}

	// run same request with the target
	defer func() {
		if r := recover(); r != nil && *debug {
			log.Warn("Recovered in f", r)
		}
	}()
	LogDebugWithTime("Bulding Target Request", requestID)
	target_parse, err := LocalParseURL(h.Target)
	if err != nil {
		LogErrorWithTime(fmt.Sprintf("Failed to parse Target: %s: %v\n", h.Target, err), requestID)
	}
	target_request.URL.Host = target_parse.Host
	target_request.URL.Scheme = target_parse.Scheme

	clientHttpConn2 := &http.Client{
		Timeout: time.Duration(time.Duration(*productionTimeout) * time.Second),
	}

	resp2, err := clientHttpConn2.Do(target_request)
	LogDebugWithTime("Target Reply Received", requestID)
	if err != nil {
		LogErrorWithTime(fmt.Sprintf("Failed to send to %s: %v\n", h.Target, err), requestID)
		return
	}
	for k, v := range resp2.Header {
		w.Header()[k] = v
	}
	body, _ := ioutil.ReadAll(resp2.Body)
	resp2.Body.Close()
	w.WriteHeader(resp2.StatusCode)
	w.Write(body)
	LogDebugWithTime("Target Reply Returned to Client", requestID)
}

func main() {
	flag.Parse()

	// We shouldn't have to worry about this is 1.5... nonetheless:
	max_workers := runtime.GOMAXPROCS(runtime.NumCPU() * 32)
	if *workers > max_workers {
		*workers = max_workers
	}

	// set up for STDOUT
	log.Info("Debugging: ", *debug)
	if *debug {
		log.SetLevel(log.DebugLevel)
	}
	log.Debug("Workers: ", *workers)
	log.Debug("Start Time: ", time.Now())

	// our queue channels
	quit := make(chan bool)
	tick := make(chan bool)
	var wg sync.WaitGroup

	// spawn workers
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go worker(tasks, quit, tick, &wg)
	}

	// set up HTTP listener
	local, err := net.Listen("tcp", *listen)
	if err != nil {
		LogErrorWithTime(fmt.Sprintf("Failed to listen to %s\n", *listen), "MAIN")
		return
	}
	h := handler{
		Target:      *targetProduction,
		Alternative: *altTarget,
	}

	// start marking time
	go func() {
		for {
			time.Sleep(time.Second * 1)
			tick <- true
		}
	}()

	// run service (NOTE: feel free to replace 'http' for 'fcgi' if you don't care about proxying headers
	http.Serve(local, h)

	// ... and our cleanup
	defer func() {
		close(quit)
		wg.Wait()
	}()
}

func DuplicateRequest(request *http.Request) (request1 *http.Request, request2 *http.Request) {
	b1 := new(bytes.Buffer)
	b2 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	io.Copy(w, request.Body)
	defer request.Body.Close()
	request1 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        request.Header,
		Body:          nopCloser{b1},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}
	request2 = &http.Request{
		Method:        request.Method,
		URL:           request.URL,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        request.Header,
		Body:          nopCloser{b2},
		Host:          request.Host,
		ContentLength: request.ContentLength,
	}
	return
}
