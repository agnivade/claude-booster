package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

func main() {
	port := flag.Int("port", 8080, "Port number to listen on")
	flag.Parse()

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("Received request %s %s %s\n", r.Method, r.Host, r.RemoteAddr)
			if r.Method == http.MethodConnect {
				handleTunneling(w, r)
			} else {
				handleHTTP(w, r)
			}
		}),
	}
	// create a listener
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(*port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Starting server on port %d", *port)
	log.Fatal(server.Serve(listener))
}

func handleTunneling(w http.ResponseWriter, r *http.Request) {
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		log.Printf("Failed to connect to host %s: %v", r.Host, err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	destTCP, ok := destConn.(*net.TCPConn)
	if !ok {
		http.Error(w, "Destination connection is not TCP", http.StatusInternalServerError)
		return
	}

	clientTCP, ok := clientConn.(*net.TCPConn)
	if !ok {
		http.Error(w, "Client connection is not TCP", http.StatusInternalServerError)
		return
	}

	go proxy(clientTCP, destTCP)
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	req.URL.Host = req.Host

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func proxy(src, dst *net.TCPConn) {
	defer src.Close()
	defer dst.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := io.Copy(src, io.TeeReader(dst, newHexDumper("[SERVER→CLIENT] ")))
		if err != nil {
			log.Printf("error copying from dst -> src: %v\n", err)
		}
		// Signal peer that no more data is coming.
		err = src.CloseWrite()
		if err != nil {
			log.Printf("error calling CloseWrite on src %v\n", err)
		}
	}()
	go func() {
		defer wg.Done()
		_, err := io.Copy(dst, io.TeeReader(src, newHexDumper("[CLIENT→SERVER] ")))
		if err != nil {
			log.Printf("error copying from src -> dst: %v\n", err)
		}
		// Signal peer that no more data is coming.
		err = dst.CloseWrite()
		if err != nil {
			log.Printf("error calling CloseWrite on dst %v\n", err)
		}
	}()

	wg.Wait()
	log.Printf("Connection finished [source: %s destination: %s]\n", src.RemoteAddr(), dst.RemoteAddr())
}

type hexDumper struct {
	prefix string
}

func newHexDumper(prefix string) *hexDumper {
	return &hexDumper{prefix: prefix}
}

func (h *hexDumper) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	fmt.Printf("%s%s\n", h.prefix, hex.Dump(data))
	return len(data), nil
}
