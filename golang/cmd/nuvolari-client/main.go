package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/bassosimone/nuvolari/golang/ndt7client"
)

var adaptive = flag.Bool("adaptive", false, "Enable adaptive test duration")
var disableTLS = flag.Bool("disable-tls", false, "Disable TLS")
var duration = flag.Int("duration", 0, "Desired duration")
var hostname = flag.String("hostname", "localhost", "Host to connect to")
var port = flag.String("port", "", "Port to connect to")
var skipTLSVerify = flag.Bool("skip-tls-verify", false, "Skip TLS verify")

type myHandler struct {
}

func (myHandler) printMeasurement(s string, m ndt7client.Measurement) {
	data, err := json.Marshal(m)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%s: %s\n", s, string(data))
}

func (myHandler) OnLogInfo(m string) {
	log.Println(m)
}

func (mh myHandler) OnServerDownloadMeasurement(m ndt7client.Measurement) {
	mh.printMeasurement("Server measurement", m)
}

func (mh myHandler) OnClientDownloadMeasurement(m ndt7client.Measurement) {
	mh.printMeasurement("Client measurement", m)
}

func main() {
	flag.Parse()
	settings := ndt7client.Settings{}
	settings.Download.Adaptive = *adaptive
	settings.DisableTLS = *disableTLS
	settings.Download.Duration = *duration
	settings.Hostname = *hostname
	settings.Port = *port
	settings.SkipTLSVerify = *skipTLSVerify
	clnt := ndt7client.Client{
		Settings: settings,
		Handler: myHandler{},
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
	err := clnt.RunDownload(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
