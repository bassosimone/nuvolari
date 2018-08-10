// Package nuvolari implements a NDTv7 client.
package nuvolari

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/m-lab/ndt-cloud/ndt7"
)

// Settings contains the NDT7 Client settings. This structure is serializable
// to JSON and allows to configure a Client from bindings.
type Settings struct {
	DisableTLS    bool   `json:"disable_tls"`     // Whether to disable TLS
	SkipTLSVerify bool   `json:"skip_tls_verify"` // Whether to skip TLS verification
	Hostname      string `json:"hostname"`        // Mandatory hostname
	Port          string `json:"port"`            // Optional port
	Duration      int    `json:"duration"`        // Optional duration (seconds)
}

// Client is a NDT7 client.
type Client struct {
	// Dialer is a WebSocket dialer. The default configuration should be good
	// in most cases. However, for testing purposes you MAY want to disable
	// TLS certificate verification by setting a custom TSLClientConfig field.
	Dialer websocket.Dialer

	// URL is the URL to use. The only required field is Host. If the Scheme
	// is empty, we will use "wss". If the Path is empty, we will use
	// the canonical path for the selected subtest (e.g. "/ndt/v7/download"
	// for the download subtest). If the Query is empty we will send no
	// query. To configure the maximum duration, you can add a "duration" value
	// to the query string (see cmd/nuvolari-server/main.go).
	URL url.URL
}

// NewClient creates a new client given specific |settings|.
func NewClient(settings Settings) (Client, error) {
	clnt := Client{}
	if settings.DisableTLS {
		clnt.URL.Scheme = "ws"
	}
	if settings.SkipTLSVerify {
		config := tls.Config{InsecureSkipVerify: true}
		clnt.Dialer.TLSClientConfig = &config
	}
	if settings.Port != "" {
		ip := net.ParseIP(settings.Hostname)
		if ip == nil || ip.To4() != nil {
			clnt.URL.Host = settings.Hostname
			clnt.URL.Host += ":"
			clnt.URL.Host += settings.Port
		} else if ip.To16() != nil {
			clnt.URL.Host = "["
			clnt.URL.Host += settings.Hostname
			clnt.URL.Host += "]:"
			clnt.URL.Host += settings.Port
		} else {
			return Client{}, errors.New("You passed me a weird settings.Hostname")
		}
	} else {
		clnt.URL.Host = settings.Hostname
	}
	if settings.Duration > 0 {
		query := clnt.URL.Query()
		query.Add("duration", strconv.Itoa(settings.Duration))
		clnt.URL.RawQuery = query.Encode()
	}
	return clnt, nil
}

// EvKey uniquely identifies an event.
type EvKey string

const (
	// LogEvent indicates an event containing a log message
	LogEvent = EvKey("log")
	// MeasurementEvent indicates an event containing some measurements
	MeasurementEvent = EvKey("ndt7.measurement")
	// FailureEvent indicates an event containing an error
	FailureEvent = EvKey("measurement.failure")
)

// Event is the structure of a generic event
type Event struct {
	Key   EvKey       `json:"key"`   // Tells you the kind of the event
	Value interface{} `json:"value"` // Opaque event value
}

// LogLevel indicates the severity of a log message
type LogLevel string

const (
	// LogWarning indicates a warning message
	LogWarning = LogLevel("warning")
	// LogInfo indicates an informational message
	LogInfo = LogLevel("info")
	// LogDebug indicates a debug message
	LogDebug = LogLevel("debug")
)

// LogRecord is the structure of a log event
type LogRecord struct {
	LogLevel LogLevel `json:"log_level"` // Message severity
	Message  string   `json:"message"`   // The message
}

// MeasurementRecord is the structure of a measurement event
type MeasurementRecord struct {
	ndt7.Measurement      // The measurement
	IsLocal          bool `json:"is_local"` // Whether it is a local measurement
}

// FailureRecord is the structure of a failure event
type FailureRecord struct {
	Failure string `json:"failure"` // The error that occurred
}

// defaultTimeout is the default value of the I/O timeout.
const defaultTimeout = 1 * time.Second

// Download runs a NDT7 download test. The |ctx| context allows the caller
// to interrupt the download early by cancelling the context.
func (cl Client) Download(ctx context.Context) chan Event {
	ch := make(chan Event)
	go func() {
		defer close(ch)
		if cl.URL.Scheme == "" {
			cl.URL.Scheme = "wss"
		}
		if cl.URL.Path == "" {
			cl.URL.Path = ndt7.DownloadURLPath
		}
		headers := http.Header{}
		headers.Add("Sec-WebSocket-Protocol", ndt7.SecWebSocketProtocol)
		cl.Dialer.HandshakeTimeout = defaultTimeout
		ch <- Event{Key: LogEvent, Value: LogRecord{LogLevel: LogInfo,
			Message: "Creating a WebSocket connection to: " + cl.URL.String()}}
		conn, _, err := cl.Dialer.Dial(cl.URL.String(), headers)
		if err != nil {
			ch <- Event{Key: FailureEvent, Value: FailureRecord{Failure: err.Error()}}
			return
		}
		conn.SetReadLimit(ndt7.MinMaxMessageSize)
		defer conn.Close()
		ch <- Event{Key: LogEvent, Value: LogRecord{LogLevel: LogInfo,
			Message: "Starting download"}}
		ticker := time.NewTicker(ndt7.MinMeasurementInterval)
		defer ticker.Stop()
		t0 := time.Now()
		count := int64(0)
		for running := true; running; {
			select {
			case t := <-ticker.C:
				ch <- Event{Key: MeasurementEvent, Value: MeasurementRecord{
					IsLocal: true, Measurement: ndt7.Measurement{
						Elapsed: t.Sub(t0).Nanoseconds(), NumBytes: count}}}
			case <-ctx.Done():
				running = false
			default: // None of the above, receive more data
				conn.SetReadDeadline(time.Now().Add(defaultTimeout))
				mtype, mdata, err := conn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
						ch <- Event{Key: FailureEvent, Value: FailureRecord{
							Failure: err.Error()}}
					}
					return
				}
				count += int64(len(mdata))
				if mtype == websocket.TextMessage {
					measurement := ndt7.Measurement{}
					err := json.Unmarshal(mdata, &measurement)
					if err != nil {
						ch <- Event{Key: FailureEvent, Value: FailureRecord{
							Failure: err.Error()}}
						return
					}
					ch <- Event{Key: MeasurementEvent, Value: MeasurementRecord{
						IsLocal: false, Measurement: measurement}}
				}
			}
		}
		ch <- Event{Key: LogEvent, Value: LogRecord{LogLevel: LogInfo,
			Message: "Download complete"}}
	}()
	return ch
}
