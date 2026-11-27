// Command mock-rcodezero serves the in-memory RcodeZero mock for manual tofu runs.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/dg-i/terraform-provider-rcodezero/internal/mockserver"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	token := flag.String("token", "mock-token", "bearer token the mock accepts")
	flag.Parse()

	h, err := mockserver.New().Handler(*token)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("RcodeZero mock listening on http://%s", *listen)
	log.Fatal(http.ListenAndServe(*listen, logRequests(h)))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
