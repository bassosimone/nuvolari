package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/apex/log"
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
	ch := make(chan interface{}, 1)
	defer close(ch)
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	if runtime.GOOS != "windows" {
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			log.Warn("Got interrupt signal")
			ch <- false
			log.Warn("Delivered interrupt signal")
		}()
	}
	for ev := range clnt.Download(ch) {
		switch ev.Key {
		case nuvolari.LogEvent:
			r := ev.Value.(nuvolari.LogRecord)
			switch r.Severity {
			case nuvolari.LogWarning:
				log.Warn(r.Message)
			case nuvolari.LogInfo:
				log.Info(r.Message)
			case nuvolari.LogDebug:
				log.Debug(r.Message)
			}
		case nuvolari.MeasurementEvent:
			r := ev.Value.(nuvolari.MeasurementRecord)
			s, e := json.Marshal(r)
			if e != nil {
				panic("Cannot serialize simple JSON")
			}
			log.Infof("Got a measurement: %s", s)
		case nuvolari.FailureEvent:
			r := ev.Value.(nuvolari.FailureRecord)
			log.WithError(r.Err).Warn("Download did not complete cleanly")
			os.Exit(1)
		}
	}
}
