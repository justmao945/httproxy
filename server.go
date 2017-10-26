package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

var L = log.New(os.Stdout, "http_proxy: ", log.Lshortfile|log.LstdFlags)

type Server struct {
	Tr *http.Transport
}

// Create and intialize
func NewServer() (self *Server, err error) {
	self = &Server{
		Tr: http.DefaultTransport.(*http.Transport),
	}
	return
}

// HTTP proxy accepts requests with following two types:
//  - CONNECT
//    Generally, this method is used when the client want to connect server with HTTPS.
//    In fact, the client can do anything he want in this CONNECT way...
//    The request is something like:
//      CONNECT www.google.com:443 HTTP/1.1
//    Only has the host and port information, and the proxy should not do anything with
//    the underlying data. What the proxy can do is just exchange data between client and server.
//    After accepting this, the proxy should response
//      HTTP/1.1 200 OK
//    to the client if the connection to the remote server is established.
//    Then client and server start to exchange data...
//
//  - non-CONNECT, such as GET, POST, ...
//    In this case, the proxy should redo the method to the remote server.
//    All of these methods should have the absolute URL that contains the host information.
//    A GET request looks like:
//      GET weibo.com/justmao945/.... HTTP/1.1
//    which is different from the normal http request:
//      GET /justmao945/... HTTP/1.1
//    Because we can be sure that all of them are http request, we can only redo the request
//    to the remote server and copy the reponse to client.
//
func (self *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	L.Printf("%s %s %s\n", r.Method, r.RequestURI, r.Proto)

	if r.Method == "CONNECT" {
		self.Connect(w, r)
	} else if r.URL.IsAbs() {
		// This is an error if is not empty on Client
		r.RequestURI = ""
		// If no Accept-Encoding header exists, Transport will add the headers it can accept
		// and would wrap the response body with the relevant reader.
		r.Header.Del("Accept-Encoding")
		// curl can add that, see
		// http://homepage.ntlworld.com/jonathan.deboynepollard/FGA/web-proxy-connection-header.html
		r.Header.Del("Proxy-Connection")
		// Connection is single hop Header:
		// http://www.w3.org/Protocols/rfc2616/rfc2616.txt
		// 14.10 Connection
		//   The Connection general-header field allows the sender to specify
		//   options that are desired for that particular connection and MUST NOT
		//   be communicated by proxies over further connections.
		r.Header.Del("Connection")
		self.HTTP(w, r)
	} else {
		L.Printf("%s is not a full URL path\n", r.RequestURI)
	}
}

// Data flow:
//  1. Receive request R1 from client
//  2. Re-post request R1 to remote server(the one client want to connect)
//  3. Receive response P1 from remote server
//  4. Send response P1 to client
func (self *Server) HTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "CONNECT" {
		L.Println("this function can not handle CONNECT method")
		http.Error(w, r.Method, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()

	// Client.Do is different from DefaultTransport.RoundTrip ...
	// Client.Do will change some part of request as a new request of the server.
	// The underlying RoundTrip never changes anything of the request.
	resp, err := self.Tr.RoundTrip(r)
	if err != nil {
		L.Printf("RoundTrip: %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// please prepare header first and write them
	CopyHeader(w, resp)
	w.WriteHeader(resp.StatusCode)

	n, err := io.Copy(w, resp.Body)
	if err != nil {
		L.Printf("Copy: %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	d := BeautifyDuration(time.Since(start))
	ndtos := BeautifySize(n)
	L.Printf("RESPONSE %s %s in %s <-%s\n", r.URL.Host, resp.Status, d, ndtos)
}

// Data flow:
//  1. Receive CONNECT request from the client
//  2. Dial the remote server(the one client want to conenct)
//  3. Send 200 OK to client if the connection is established
//  4. Exchange data between client and server
func (self *Server) Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "CONNECT" {
		L.Println("this function can only handle CONNECT method")
		http.Error(w, r.Method, http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()

	// Use Hijacker to get the underlying connection
	hij, ok := w.(http.Hijacker)
	if !ok {
		s := "Server does not support Hijacker"
		L.Println(s)
		http.Error(w, s, http.StatusInternalServerError)
		return
	}

	src, _, err := hij.Hijack()
	if err != nil {
		L.Printf("Hijack: %s\n", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer src.Close()

	// connect the remote client directly
	dst, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		L.Printf("Dial: %s\n", err.Error())
		src.Write([]byte("HTTP/1.1 500 Internal Server Error\r\n\r\n"))
		return
	}
	defer dst.Close()

	// Once connected successfully, return OK
	src.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))

	// Proxy is no need to know anything, just exchange data between the client
	// the the remote server.
	copyAndWait := func(dst, src net.Conn, c chan int64) {
		n, err := io.Copy(dst, src)
		if err != nil {
			L.Printf("Copy: %s\n", err.Error())
			// FIXME: how to report error to dst ?
		}
		dst.Close()
		c <- n
	}

	// client to remote
	stod := make(chan int64)
	go copyAndWait(dst, src, stod)

	// remote to client
	dtos := make(chan int64)
	go copyAndWait(src, dst, dtos)

	// Generally, the remote server would keep the connection alive,
	// so we will not close the connection until both connection recv
	// EOF and are done!
	nstod, ndtos := BeautifySize(<-stod), BeautifySize(<-dtos)
	d := BeautifyDuration(time.Since(start))
	L.Printf("CLOSE %s after %s ->%s <-%s\n", r.URL.Host, d, nstod, ndtos)
}
