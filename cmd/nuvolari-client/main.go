package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/bassosimone/nuvolari"
)

var disableTLS = flag.Bool("disable-tls", false, "Whether to disable TLS")
var duration = flag.Int("duration", 0, "Desired duration")
var hostname = flag.String("hostname", "localhost", "Host to connect to")
var port = flag.String("port", "", "Port to connect to")
var skipTLSVerify = flag.Bool("skip-tls-verify", false, "Skip TLS verify")

func main() {
	flag.Parse()
	settings := nuvolari.Settings{}
	settings.DisableTLS = *disableTLS
	settings.Duration = *duration
	settings.Hostname = *hostname
	settings.Port = *port
	settings.SkipTLSVerify = *skipTLSVerify
	clnt, err := nuvolari.NewClient(settings)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	if runtime.GOOS != "windows" {
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs   // Wait for a signal to appear
			cancel() // Cancel pending download
		}()
	}
	rv := 0
	for ev := range clnt.Download(ctx) {
		if ev.Key == nuvolari.FailureEvent {
			rv = 1 // if we have seen an error be prepared to os.Exit(1)
		}
		data, err := json.Marshal(ev)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(data))
	}
	os.Exit(rv)
}
