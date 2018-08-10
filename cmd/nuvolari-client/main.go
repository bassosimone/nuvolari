package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/bassosimone/nuvolari"
)

var disableTLS = flag.Bool("disable-tls", false, "Whether to disable TLS")
var duration = flag.Int("duration", 10, "Desired duration")
var hostname = flag.String("hostname", "localhost", "Host to connect to")
var port = flag.String("port", "3001", "Port to connect to")
var skipTLSVerify = flag.Bool("skip-tls-verify", false, "Skip TLS verify")

func main() {
	flag.Parse()
	settings := nuvolari.Settings{}
	settings.Hostname = *hostname
	settings.InsecureNoTLS = *disableTLS
	settings.InsecureSkipTLSVerify = *skipTLSVerify
	settings.Port = *port
	settings.Duration = *duration
	clnt := nuvolari.NewClient(settings)
	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	if runtime.GOOS != "windows" {
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			fmt.Println("Got interrupt signal")
			cancel()
			fmt.Println("Delivered interrupt signal")
		}()
	}
	rv := 0
	for ev := range clnt.Download(ctx) {
		if ev.Key == nuvolari.FailureEvent {
			rv = 1  // if we have seen an error be prepared to os.Exit(1)
		}
		data, err := json.Marshal(ev)
		if err != nil {
			panic("Cannot serialize event as JSON")
		}
		fmt.Println(string(data))
	}
	os.Exit(rv)
}
