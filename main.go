package main

import (
	"flag"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "serve address")
	flag.Parse()

	srv, err := NewServer()
	if err != nil {
		L.Fatalln(err)
	}

	L.Printf("Listen on: %s\n", *addr)
	L.Fatalln(http.ListenAndServe(*addr, srv))
}
