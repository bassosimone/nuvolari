package main

import (
	"flag"
	"net/http"

	"github.com/apex/log"
	"github.com/bassosimone/nuvolari"
)

var address = flag.String("address", "127.0.0.1:3001", "Address to listen to")

func main() {
	flag.Parse()
	dl := nuvolari.DownloadHandler{}
	http.HandleFunc(nuvolari.DownloadURLPath, dl.Handle)
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)
	err := http.ListenAndServe(*address, nil)
	if err != nil {
		log.WithError(err).Fatal("http.ListenAndServe() failed")
	}
}
