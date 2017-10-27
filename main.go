package main

import (
	"bytes"
	"flag"
	httproxy "github.com/justmao945/httproxy/http"
	socks5 "github.com/justmao945/httproxy/socks"
	"io"
	"log"
	"net"
	"time"
)

const (
	socks5Version = uint8(5)
)

type combConn struct {
	net.Conn
	r io.Reader
}

func (c combConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

var socks5Server = socks5.New(&socks5.Config{})

func handConn(conn net.Conn) {
	firstByte := []byte{0}
	_, err := conn.Read(firstByte)
	if err != nil {
		log.Printf("read fist byte failed: %v\n", err)
		return
	}

	conn2 := combConn{
		conn,
		io.MultiReader(bytes.NewReader(firstByte), conn),
	}

	if firstByte[0] == socks5Version {
		socks5Server.ServeConn(conn2)
	} else {
		httproxy.ServeConn(conn2)
	}
}

func main() {
	var flagAddr string
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	flag.StringVar(&flagAddr, "addr", ":8080", "serve HTTP and SOCKS5 proxy address")
	flag.Parse()

	listener, err := net.Listen("tcp", flagAddr)
	if err != nil {
		log.Fatalf("listen failed: %v\n", err)
	}

	log.Printf("listen HTTP and SOCKS5 proxy on %v\n", flagAddr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept failed: %v\n", err)
			if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return // fatal error, stop
		}

		go handConn(conn)
	}
}
