package http

import (
	"bytes"
	"github.com/valyala/fasthttp"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var L = log.New(os.Stderr, "http: ", log.Lshortfile|log.LstdFlags)

var bufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024)
	},
}

type closeWriter interface {
	CloseWrite() error
}

type connector struct {
	remoteAddr string
}

func newConnector(host string) connector {
	_, _, err := net.SplitHostPort(host)
	if err != nil && strings.Contains(err.Error(), "missing port in address") {
		host += ":443"
	}
	return connector{
		remoteAddr: host,
	}
}

func (c connector) connect(src net.Conn) {
	start := time.Now()

	dst, err := net.Dial("tcp", c.remoteAddr)
	if err != nil {
		L.Printf("Dial: %s\n", err.Error())
		src.Write([]byte("HTTP/1.1 500 proxy error\r\n\r\n"))
		return
	}
	defer dst.Close()

	// Proxy is no need to know anything, just exchange data between the client
	// the the remote server.
	copyAndWait := func(dst, src net.Conn, c chan int64) {
		buf := bufPool.Get().([]byte) // smaller buf smaller latency
		n, err := io.CopyBuffer(dst, src, buf)
		bufPool.Put(buf)
		if err != nil {
			L.Printf("Copy: %s\n", err.Error())
			// FIXME: how to report error to dst ?
		}
		if tcpConn, ok := dst.(closeWriter); ok {
			tcpConn.CloseWrite()
		}
		c <- n
	}

	// client to remote
	stod := make(chan int64)
	go copyAndWait(dst, src, stod)

	// remote to client
	dtos := make(chan int64)
	go copyAndWait(src, dst, dtos)

	var nstod, ndtos int64
	for i := 0; i < 2; {
		select {
		case nstod = <-stod:
			i++
		case ndtos = <-dtos:
			i++
		}
	}
	d := BeautifyDuration(time.Since(start))
	L.Printf("CLOSE %s after %s ->%s <-%s\n",
		c.remoteAddr, d, BeautifySize(nstod), BeautifySize(ndtos))
}

func doHttp(ctx *fasthttp.RequestCtx) {
	header := ctx.Request.Header
	header.Del("Proxy-Connection")
	header.Del("Connection")
	err := fasthttp.Do(&ctx.Request, &ctx.Response)
	if err != nil {
		log.Printf("do http failed: %v\n", err)
	}
}

func ServeFastHTTP(ctx *fasthttp.RequestCtx) {
	L.Printf("%s %s\n", ctx.Method(), ctx.RequestURI())

	if bytes.Equal(ctx.Method(), []byte("CONNECT")) {
		var c = newConnector(string(ctx.URI().Host()))
		ctx.Hijack(c.connect)
		ctx.Write([]byte{}) // close stream and do hijack
	} else if uri := ctx.URI(); len(uri.Host()) > 0 {
		doHttp(ctx)
	} else {
		L.Printf("%s is not a full URL path\n", ctx.URI())
	}
}

func ServeConn(conn net.Conn) {
	err := fasthttp.ServeConn(conn, ServeFastHTTP)
	if err != nil {
		log.Printf("serve failed: %v\n", err)
	}
}
