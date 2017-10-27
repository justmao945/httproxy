package main

import (
	"flag"
	httproxy "github.com/justmao945/httproxy/http"
	socks5 "github.com/justmao945/httproxy/socks"
	"log"
	"net/http"
)

func main() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	httpAddr := flag.String("http", ":8080", "serve HTTP proxy address")
	socksAddr := flag.String("socks", ":1080", "serve SOCKS5 proxy address")
	flag.Parse()

	wait := make(chan int)

	go func() {
		server, err := socks5.New(&socks5.Config{})
		if err != nil {
			log.Fatalln(err)
		}
		log.Printf("Listen SOCKS5 proxy on: %s\n", *socksAddr)
		log.Fatalln(server.ListenAndServe("tcp", *socksAddr))
		close(wait)
	}()

	go func() {
		srv, err := httproxy.NewServer()
		if err != nil {
			log.Fatalln(err)
		}
		log.Printf("Listen HTTP proxy on: %s\n", *httpAddr)
		log.Fatalln(http.ListenAndServe(*httpAddr, srv))
		close(wait)
	}()

	<-wait
}
