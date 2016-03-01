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
)


func LogDebugWithTime (s string, r string){
	log.WithFields(log.Fields{
    		"Timestamp": time.Now(),
		"request_id": r,
	}).Debug(s)
}


func LogErrorWithTime (s string, r string){
	log.WithFields(log.Fields{
    		"Timestamp": time.Now(),
		"request_id": r,
	}).Error(s)
}


// Console flags
var (
	listen            = flag.String("l", ":8888", "port to accept requests")
	targetProduction  = flag.String("a", "localhost:8080", "where production traffic goes. http://localhost:8080/production")
	altTarget         = flag.String("b", "localhost:8081", "where testing traffic goes. response are skipped. http://localhost:8081/test")
	debug             = flag.Bool("debug", false, "more logging, showing ignored output")
	productionTimeout = flag.Int("a.timeout", 3, "timeout in seconds for production traffic")
	alternateTimeout  = flag.Int("b.timeout", 3, "timeout in seconds for alternate site traffic")
)

// handler contains the address of the main Target and the one for the Alternative target
type handler struct {
	Target      string
	Alternative string
}

func LocalParseURL (rawurl string) (u *url.URL, err error)  {
	if !strings.Contains(rawurl, "http://") && !strings.Contains(rawurl, "https://"){
		rawurl = "http://"+rawurl
	}
	return url.Parse(rawurl)
}

func RandomString(strlen int) string {
	rand.Seed(time.Now().UTC().UnixNano())
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, strlen)
	for i := 0; i < strlen; i++ {
		result[i] = chars[rand.Intn(len(chars))]
	}
	return string(result)
}

func MakeRequestID() (id string){
	return RandomString(32)
}

var ops  uint64 = 0

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	atomic.AddUint64(&ops, 1)
	thisop := atomic.LoadUint64(&ops)
	requestID := MakeRequestID()
	LogDebugWithTime(fmt.Sprintf("New Request Op: %d", thisop), requestID)

	log.WithFields(
		log.Fields{
			"request_id": requestID,
			"request_method": req.Method,
			"request_path": req.URL.RequestURI(),
		}).Debug("Incomming Request")

	req1, req2 := DuplicateRequest(req)
	go func() {
		defer func() {
			if r := recover(); r != nil && *debug {
				log.Warn("Recovered in f", r)
			}
		}()
		LogDebugWithTime("Bulding Alternate Request", requestID)
		p, err := LocalParseURL(h.Alternative)
		if err != nil{
			LogErrorWithTime(fmt.Sprintf("Failed to parse Target: %s: %v\n", h.Alternative, err), requestID)
		}
		req1.URL.Scheme = p.Scheme
		req1.URL.Host = p.Host
		log.Debug(fmt.Sprintf("Alternative Scheme: %s; Alternative Host: %s\n", p.Scheme, p.Host))
		clientHttpConn1 := &http.Client{
			Timeout: time.Duration(time.Duration(*alternateTimeout)*time.Second),
		}
		_, err = clientHttpConn1.Do(req1)
		 runtime.Gosched()
		if err != nil {
			LogErrorWithTime(fmt.Sprintf("Failed to send to %s: %v\n", h.Target, err), requestID)
			return
		}
		LogDebugWithTime("Altnernate Request Finished", requestID)
	}()

	defer func() {
		if r := recover(); r != nil && *debug {
			log.Warn("Recovered in f", r)
		}
	}()

	LogDebugWithTime("Bulding Target Request", requestID)
	p, err := LocalParseURL(h.Target)
	if err != nil {
		LogErrorWithTime(fmt.Sprintf("Failed to parse Target: %s: %v\n", h.Target, err), requestID)
	}
	req2.URL.Host = p.Host
	req2.URL.Scheme = p.Scheme

	log.Debug(fmt.Sprintf("Target Scheme: %s; Target Host: %s\n", p.Scheme, p.Host))

	clientHttpConn2 := &http.Client{
		Timeout: time.Duration(time.Duration(*productionTimeout) * time.Second),
	}
	resp2, err := clientHttpConn2.Do(req2)
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

	runtime.GOMAXPROCS(runtime.NumCPU() * 32)
	log.Info("Debugging: ", *debug)
	if *debug{
		log.SetLevel(log.DebugLevel)
	}
	log.Debug("Start Time: ", time.Now())

	local, err := net.Listen("tcp", *listen)
	if err != nil {
		LogErrorWithTime(fmt.Sprintf("Failed to listen to %s\n", *listen), "MAIN")
		return
	}
	h := handler{
		Target:      *targetProduction,
		Alternative: *altTarget,
	}
	http.Serve(local, h)
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

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
