// Package nuvolari implements a ndt7 client. The specification of ndt7 is
// available at https://github.com/m-lab/ndt-cloud/blob/master/spec/ndt7.md.
package nuvolari

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// Settings contains the ndt7 Client settings.
type Settings struct {
	// Hostname is the hostname of the ndt7 server.
	Hostname string

	// Port is the port of the ndt7 server.
	Port string

	// SkipTLSVerify indicates whether we should skip TLS verify.
	SkipTLSVerify bool
}

// BBRInfo contains BBR information.
type BBRInfo struct {
	// MaxBandwidth is the bandwidth measured in bits per second.
	MaxBandwidth float64 `json:"max_bandwidth"`

	// MinRTT is the round-trip time measured in milliseconds.
	MinRTT float64 `json:"min_rtt"`
}

// Measurement is a performance measurement.
type Measurement struct {
	// Elapsed is the number of seconds elapsed since the beginning.
	Elapsed float64 `json:"elapsed"`

	// BBRInfo is optional BBR information included when possible.
	BBRInfo *BBRInfo `json:"bbr_info,omitempty"`
}

// Handler handles Client events.
type Handler interface {
	// OnLogInfo receives an informational message.
	OnLogInfo(string)

	// OnServerDownloadMeasurement receives a server-side download measurement.
	OnServerDownloadMeasurement(Measurement)

	// OnClientDownloadMeasurement receives a client-side download measurement.
	OnClientDownloadMeasurement(Measurement)
}

// Client is the default client implementation.
type Client struct {
	// Settings contains client settings.
	Settings Settings

	// Handler for events.
	Handler Handler
}

const downloadURLPath = "/ndt/v7/download"

const uploadURLPath = "/ndt/v7/upload"

// ErrInvalidHostname is returned when Settings.Hostname is invalid.
var ErrInvalidHostname = errors.New("Hostname is invalid")

func (cl Client) makeURL(path string) (url.URL, error) {
	var u url.URL
	u.Scheme = "wss"
	if cl.Settings.Port != "" {
		ip := net.ParseIP(cl.Settings.Hostname)
		if ip == nil || ip.To4() != nil {
			u.Host = cl.Settings.Hostname
			u.Host += ":"
			u.Host += cl.Settings.Port
		} else if ip.To16() != nil {
			u.Host = "["
			u.Host += cl.Settings.Hostname
			u.Host += "]:"
			u.Host += cl.Settings.Port
		} else {
			return url.URL{}, ErrInvalidHostname
		}
	} else {
		u.Host = cl.Settings.Hostname
	}
	u.Path = path
	return u, nil
}

func (cl Client) makeDialer() websocket.Dialer {
  var d websocket.Dialer
	if cl.Settings.SkipTLSVerify {
		config := tls.Config{InsecureSkipVerify: true}
		d.TLSClientConfig = &config
	}
	return d
}

const defaultDuration = 10

const defaultTimeout = 7 * time.Second

const secWebSocketProtocol = "net.measurementlab.ndt.v7"

const minMeasurementInterval = 250 * time.Millisecond

const minMaxMessageSize = 1 << 17

// ErrServerGoneWild is returned when the server runs a download for too much
// time, so that it's proper to stop the download from the client side.
var ErrServerGoneWild = errors.New("Server is running for too much time")

// RunDownload runs a ndt7 download test.
func (cl Client) RunDownload(ctx context.Context) error {
	wsURL, err := cl.makeURL(downloadURLPath)
	if err != nil {
		return err
	}
	wsDialer := cl.makeDialer()
	headers := http.Header{}
	headers.Add("Sec-WebSocket-Protocol", secWebSocketProtocol)
	wsDialer.HandshakeTimeout = defaultTimeout
	if cl.Handler != nil {
		cl.Handler.OnLogInfo("Connecting to: " + wsURL.String())
	}
	conn, _, err := wsDialer.Dial(wsURL.String(), headers)
	if err != nil {
		return err
	}
	conn.SetReadLimit(minMaxMessageSize)
	defer conn.Close()
	if cl.Handler != nil {
		cl.Handler.OnLogInfo("Connection established")
	}
	t0 := time.Now()
	tLast := t0
	count := int64(0)
	maxDuration := float64(time.Duration(defaultDuration)*time.Second) * 1.5
	for {
		// Check whether the user interrupted us
		select {
		case <-ctx.Done():
			if cl.Handler != nil {
				cl.Handler.OnLogInfo("Download interrupted by user")
			}
			return nil  // No error because user interrupted us
		default:
			break
		}
		// Check whether we've run for too much time
		now := time.Now()
		elapsed := now.Sub(t0)
		if float64(elapsed) >= maxDuration {
			return ErrServerGoneWild
		}
		// Check whether it's time to run the next client-side measurement
		if now.Sub(tLast) >= minMeasurementInterval {
			if cl.Handler != nil {
				cl.Handler.OnClientDownloadMeasurement(Measurement{
					Elapsed: elapsed.Seconds(),
				})
			}
			tLast = now
		}
		// Read and process the next WebSocket message
		conn.SetReadDeadline(time.Now().Add(defaultTimeout))
		mtype, mdata, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return err
			}
			break
		}
		count += int64(len(mdata))
		if mtype == websocket.TextMessage {
			var measurement Measurement
			err := json.Unmarshal(mdata, &measurement)
			if err != nil {
				return err
			}
			if cl.Handler != nil {
				cl.Handler.OnServerDownloadMeasurement(measurement)
			}
		}
	}
	return nil
}

// makePreparedMessage generates a prepared message that should be sent
// over the network for generating network load.
func makePreparedMessage(size int) (*websocket.PreparedMessage, error) {
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	data := make([]byte, size)
	// This is not the fastest algorithm to generate a random string, yet it
	// is most likely good enough for our purposes. See [1] for a comprehensive
	// discussion regarding how to generate a random string in Golang.
	//
	// .. [1] https://stackoverflow.com/a/31832326/4354461
	for i := range data {
		data[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return websocket.NewPreparedMessage(websocket.BinaryMessage, data)
}

// RunUpload runs a ndt7 upload test.
func (cl Client) RunUpload(ctx context.Context) error {
	// TODO(bassosimone): factor out the duplicate code below
	wsURL, err := cl.makeURL(uploadURLPath)
	if err != nil {
		return err
	}
	wsDialer := cl.makeDialer()
	headers := http.Header{}
	headers.Add("Sec-WebSocket-Protocol", secWebSocketProtocol)
	wsDialer.HandshakeTimeout = defaultTimeout
	if cl.Handler != nil {
		cl.Handler.OnLogInfo("Connecting to: " + wsURL.String())
	}
	conn, _, err := wsDialer.Dial(wsURL.String(), headers)
	if err != nil {
		return err
	}
	conn.SetReadLimit(minMaxMessageSize)
	defer conn.Close()
	if cl.Handler != nil {
		cl.Handler.OnLogInfo("Connection established")
	}
	t0 := time.Now()
	maxDuration := float64(time.Duration(defaultDuration)*time.Second)
	const bulkMessageSize = 1 << 13
	preparedMessage, err := makePreparedMessage(bulkMessageSize)
	if err != nil {
		return err
	}
	for {
		// Check whether the user interrupted us
		select {
		case <-ctx.Done():
			if cl.Handler != nil {
				cl.Handler.OnLogInfo("Upload interrupted by user")
			}
			return nil  // No error because user interrupted us
		default:
			break
		}
		// Check whether we've run for too much time
		now := time.Now()
		elapsed := now.Sub(t0)
		if float64(elapsed) >= maxDuration {
			break
		}
		if err := conn.WritePreparedMessage(preparedMessage); err != nil {
			return err
		}
	}
	return conn.Close()
}
