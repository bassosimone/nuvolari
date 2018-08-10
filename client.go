// Package nuvolari implements a NDTv7 client.
package nuvolari

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/m-lab/ndt-cloud/ndt7"
)

// Settings contains client settings. All settings are optional except for
// the Hostname, which cannot be autoconfigured at the moment.
type Settings struct {
	// This structure embeds options defined in the spec.
	ndt7.Options
	// InsecureSkipTLSVerify can be used to disable certificate verification.
	InsecureSkipTLSVerify bool
	// InsecureNoTLS can be used to force using cleartext.
	InsecureNoTLS bool
	// Hostname is the hostname of the NDT7 server to connect to.
	Hostname string
	// Port is the port of the NDT7 server to connect to.
	Port string
}

// Client is a NDT7 client.
type Client struct {
	dialer websocket.Dialer
	url    url.URL
}

// NewClient creates a new client.
func NewClient(settings Settings) Client {
	cl := Client{}
	if settings.InsecureSkipTLSVerify {
		config := tls.Config{InsecureSkipVerify: true}
		cl.dialer.TLSClientConfig = &config
	}
	if settings.InsecureNoTLS {
		cl.url.Scheme = "ws"
	} else {
		cl.url.Scheme = "wss"
	}
	if settings.Port != "" {
		ip := net.ParseIP(settings.Hostname)
		if ip == nil || len(ip) == 4 {
			cl.url.Host = settings.Hostname
			cl.url.Host += ":"
			cl.url.Host += settings.Port
		} else if len(ip) == 16 {
			cl.url.Host = "["
			cl.url.Host += settings.Hostname
			cl.url.Host += "]:"
			cl.url.Host += settings.Port
		} else {
			panic("IP address that is neither 4 nor 16 bytes long")
		}
	} else {
		cl.url.Host = settings.Hostname
	}
	query := cl.url.Query()
	if settings.Duration > 0 {
		query.Add("duration", strconv.Itoa(settings.Duration))
	}
	cl.url.RawQuery = query.Encode()
	return cl
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
		ch <- Event{Key: LogEvent, Value: LogRecord{LogLevel: LogInfo,
			Message: "Creating a WebSocket connection"}}
		cl.url.Path = ndt7.DownloadURLPath
		headers := http.Header{}
		headers.Add("Sec-WebSocket-Protocol", ndt7.SecWebSocketProtocol)
		cl.dialer.HandshakeTimeout = defaultTimeout
		conn, _, err := cl.dialer.Dial(cl.url.String(), headers)
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
				break
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
