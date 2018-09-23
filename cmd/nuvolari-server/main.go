package main

import (
	"flag"
	"net/http"

	"github.com/apex/log"
	"github.com/m-lab/ndt-cloud/ndt7"
)

var address = flag.String("address", "127.0.0.1:3001", "Address to listen to")

func main() {
	flag.Parse()
	http.Handle(ndt7.DownloadURLPath, ndt7.DownloadHandler{})
	fs := http.FileServer(http.Dir("www"))
	http.Handle("/", fs)
	err := http.ListenAndServe(*address, nil)
	if err != nil {
		log.WithError(err).Fatal("http.ListenAndServe() failed")
	}
}
