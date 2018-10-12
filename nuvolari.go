// Package nuvolari implements a ndt7 client. The specification of ndt7 is
// available at https://github.com/m-lab/ndt-cloud/blob/master/spec/ndt7.md.
package nuvolari

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// DownloadSettings contains ndt7 Client settings pertaining to the download.
type DownloadSettings struct {
	// Adaptive indicates whether the server is allowed to terminate the
	// download early if BBR converges before the configured duration.
	Adaptive bool

	// Duration indicates an optional duration expressed in seconds.
	Duration int
}

// Settings contains the ndt7 Client settings.
type Settings struct {
	// DisableTLS indicates whether we should disable TLS.
	DisableTLS bool

	// Hostname is the hostname of the ndt7 server.
	Hostname string

	// Port is the port of the ndt7 server.
	Port string

	// SkipTLSVerify indicates whether we should skip TLS verify.
	SkipTLSVerify bool

	// Download contains settings controlling the download.
	Download DownloadSettings

	// Scramble controls whether to turn on scrambling with PSK when
	// DisableTLS is true.
	Scramble bool
}

// BBRInfo contains BBR information.
type BBRInfo struct {
	// Bandwidth is the bandwidth measured in bits per second.
	Bandwidth float64 `json:"bandwidth"`

	// RTT is the round-trip time measured in milliseconds.
	RTT float64 `json:"rtt"`
}

// Measurement is a performance measurement.
type Measurement struct {
	// Elapsed is the number of seconds elapsed since the beginning.
	Elapsed float64 `json:"elapsed"`

	// Bytes is the number of bytes transmitted since the beginning.
	NumBytes int64 `json:"num_bytes"`

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

// ErrInvalidHostname is returned when Settings.Hostname is invalid.
var ErrInvalidHostname = errors.New("Hostname is invalid")

func (cl Client) makeURL() (url.URL, error) {
	var u url.URL
	if cl.Settings.DisableTLS {
		u.Scheme = "ws"
	} else {
		u.Scheme = "wss"
	}
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
	u.Path = downloadURLPath
	query := u.Query()
	if cl.Settings.Download.Duration > 0 {
		query.Add("duration", strconv.Itoa(cl.Settings.Download.Duration))
	}
	if cl.Settings.Download.Adaptive {
		query.Add("adaptive", strconv.FormatBool(cl.Settings.Download.Adaptive))
	}
	u.RawQuery = query.Encode()
	return u, nil
}

type scrambleConn struct {
	net.Conn
}

func newscrambleConn(conn net.Conn) net.Conn {
	return scrambleConn{Conn: conn}
}

func (sconn scrambleConn) Read(b []byte) (int, error) {
	n, e := sconn.Conn.Read(b)
	if e != nil {
		return 0, nil
	}
	for i := 0; i < n; i += 1 {
		b[i] ^= 0x34
	}
	return n, nil
}

func (sconn scrambleConn) Write(b []byte) (int, error) {
	c := make([]byte, len(b))
	for i := 0; i < len(b); i += 1 {
		c[i] = b[i] ^ 0x34
	}
	return sconn.Conn.Write(c)
}

func scrambleDial(network, addr string) (net.Conn, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	log.Print("SCRAMBLED CONN")
	return newscrambleConn(conn), nil
}

func (cl Client) makeDialer() websocket.Dialer {
  var d websocket.Dialer
	if cl.Settings.SkipTLSVerify {
		config := tls.Config{InsecureSkipVerify: true}
		d.TLSClientConfig = &config
	}
	if cl.Settings.DisableTLS && cl.Settings.Scramble {
		d.NetDial = scrambleDial
	}
	return d
}

const defaultDuration = 10

const defaultTimeout = 3 * time.Second

const secWebSocketProtocol = "net.measurementlab.ndt.v7"

const minMeasurementInterval = 250 * time.Millisecond

const minMaxMessageSize = 1 << 17

// ErrServerGoneWild is returned when the server runs a download for too much
// time, so that it's proper to stop the download from the client side.
var ErrServerGoneWild = errors.New("Server is running for too much time")

// RunDownload runs a ndt7 download test.
func (cl Client) RunDownload(ctx context.Context) error {
	wsURL, err := cl.makeURL()
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
	duration := cl.Settings.Download.Duration
	if duration <= 0 {
		duration = defaultDuration
	}
	maxDuration := time.Duration(duration)*2*time.Second
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
		if elapsed >= maxDuration {
			return ErrServerGoneWild
		}
		// Check whether it's time to run the next client-side measurement
		if now.Sub(tLast) >= minMeasurementInterval {
			if cl.Handler != nil {
				cl.Handler.OnClientDownloadMeasurement(Measurement{
					Elapsed: elapsed.Seconds(),
					NumBytes: count,
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
