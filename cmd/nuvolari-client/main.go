package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
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
	clnt := nuvolari.Client{}
	if *skipTLSVerify {
		config := tls.Config{InsecureSkipVerify: true}
		clnt.Dialer.TLSClientConfig = &config
	}
	if *disableTLS {
		clnt.URL.Scheme = "ws"
	}
	if *port != "" {
		ip := net.ParseIP(*hostname)
		if ip == nil || len(ip) == 4 {
			clnt.URL.Host = *hostname
			clnt.URL.Host += ":"
			clnt.URL.Host += *port
		} else if len(ip) == 16 {
			clnt.URL.Host = "["
			clnt.URL.Host += *hostname
			clnt.URL.Host += "]:"
			clnt.URL.Host += *port
		} else {
			panic("IP address that is neither 4 nor 16 bytes long")
		}
	} else {
		clnt.URL.Host = *hostname
	}
	if *duration > 0 {
		query := clnt.URL.Query()
		query.Add("duration", strconv.Itoa(*duration))
		clnt.URL.RawQuery = query.Encode()
	}
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
