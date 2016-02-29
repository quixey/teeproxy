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
)


func LogWithTime (s string, r string){
	log.WithFields(log.Fields{
    		"Timestamp": time.Now(),
		"request_id": r,
	}).Debug(s)
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

// ServeHTTP duplicates the incoming request (req) and does the request to the Target and the Alternate target discading the Alternate response
func (h handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	requestID := MakeRequestID()
	LogWithTime("New Request", requestID)

	req1, req2 := DuplicateRequest(req)
	go func() {
		defer func() {
			if r := recover(); r != nil && *debug {
				log.Warn("Recovered in f", r)
			}
		}()
		LogWithTime("Bulding Alternate Request", requestID)
		p, err := LocalParseURL(h.Alternative)
		if err != nil{
			log.Error("Failed to parse Target: %s: %v\n", h.Alternative, err)
		}
		req1.URL.Scheme = p.Scheme
		req1.URL.Host = p.Host
		log.Debug("Alternative Scheme: %s; Alternative Host: %s\n", p.Scheme, p.Host)
		clientHttpConn1 := &http.Client{
			Timeout: time.Duration(time.Duration(*alternateTimeout)*time.Second),
		}
		_, err = clientHttpConn1.Do(req1)
		if err != nil {
			log.Error("Failed to send to %s: %v\n", h.Target, err)
			return
		}
		LogWithTime("Altnernate Request Finished", requestID)
	}()

	defer func() {
		if r := recover(); r != nil && *debug {
			log.Warn("Recovered in f", r)
		}
	}()
	LogWithTime("Bulding Target Request", requestID)
	p, err := LocalParseURL(h.Target)
	if err != nil {
		log.Error("Failed to parse Target: %s: %v\n", h.Target, err)
	}
	req2.URL.Host = p.Host
	req2.URL.Scheme = p.Scheme

	log.Debug("Target Scheme: %s; Target Host: %s\n", p.Scheme, p.Host)

	clientHttpConn2 := &http.Client{
		Timeout: time.Duration(time.Duration(*productionTimeout) * time.Second),
	}
	resp2, err := clientHttpConn2.Do(req2)
	LogWithTime("Target Reply Received", requestID)
	if err != nil {
		log.Error("Failed to send to %s: %v\n", h.Target, err)
		return
	}
	for k, v := range resp2.Header {
		w.Header()[k] = v
	}
	body, _ := ioutil.ReadAll(resp2.Body)
	resp2.Body.Close()
	w.WriteHeader(resp2.StatusCode)
	w.Write(body)
	LogWithTime("Target Reply Sent", requestID)
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
		log.Error("Failed to listen to %s\n", *listen)
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
