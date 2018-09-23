// Package bbr contains code required to read BBR variables of a net.Conn
// on which we're serving a WebSocket client. This code currently only
// works on Linux systems, as BBR is only available there.
//
// To read BBR variables, we need a file descriptor. When serving a WebSocket
// client we have a websocket.Conn instance. The UnderlyingConn() method allows
// us to get the corresponding net.Conn, which typically is a tls.Conn. Yet,
// obtaining a file descriptor from a tls.Conn seems complex, because the
// underlying socket connection is private. Still, we've a custom listener that
// is required to turn on BBR (see tcpListenerEx). At that point, we can
// obtain a *os.File from the *net.TCPConn. From such *os.File, we can then
// get a file descriptor. However, there are some complications:
//
// a) the returned *os.File is bound to another file descriptor that is
//    a dup() of the one inside the *net.Conn, so, we should keep that
//    *os.File alive during the whole NDT7 measurement;
//
// b) in Go < 1.11, using this technique makes the file descriptor use
//    blocking I/O, which spawns more threads (see below).
//
// For these reasons, we're keeping a cache mapping between the socket's four
// tuple (i.e. local and remote address and port) and the *os.File. We use the
// four tuple because, in principle, a server can be serving on more than a
// single local IP, so only using the remote endpoint may not be enough.
//
// In the good case, this is what is gonna happen:
//
// 1. a connection is accepted in tcpListenerEx, so we have a *net.TCPConn;
//
// 2. using the *net.Conn, we turn on BBR and cache the *os.File using
//    bbr.EnableAndRememberFile() with the four tuple as the key;
//
// 3. WebSocket negotiation is successful, so we have a websocket.Conn, from
//    which we can get the underlying connection and hence the four tuple;
//
// 4. using the four tuple, we can retrieve the *os.File, removing it from
//    the cache using bbr.GetAndForgetFile();
//
// 5. we defer *os.File.Close() until the end of the WebSocket serving loop and
//    periodically we use such file to obtain the file descriptor and read the
//    BBR variables using bbr.GetBandwidthAndRTT().
//
// Because a connection might be closed between steps 2. and 3. (i.e. after
// the connection is accepted and before the HTTP layer finishes reading the
// request and determines that it should be routed to the handler that we
// have configured), we also need a stale entry management mechanism so that
// we delete *os.File instances cached for too much time.
//
// Depending on whether Golang calls shutdown() when a socket is closed or
// not, it might be that this caching mechanism keeps connections alive for
// more time than expected. The specific case where we can have this issue
// is the one where we receive a HTTP connection that is not a valid UPGRADE
// request, but a valid HTTP request. To avoid this issue, we SHOULD make
// sure to remove the *os.File from the cache basically everytime we got our
// handler called, regardless of whether the request is a valid UPGRADE.
package bbr

import (
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// ErrNoSupport indicates that this system does not support BBR.
var ErrNoSupport = errors.New("No support for BBR")

// connKey is the key associated to a TCP connection.
type connKey string

// makekey creates a connKey from |conn|.
func makekey(conn net.Conn) connKey {
	return connKey(conn.LocalAddr().String() + "<=>" + conn.RemoteAddr().String())
}

// entry is an entry inside the cache.
type entry struct {
	Fp    *os.File
	Stamp time.Time
}

// cache maps a connKey to the corresponding *os.File.
var cache map[connKey]entry = make(map[connKey]entry)

// mutex serializes access to cache.
var mutex sync.Mutex

// lastCheck is last time when we checked the cache for stale entries.
var lastCheck time.Time

// checkInterval is the interval between each check for stale entries.
const checkInterval = 500 * time.Millisecond

// maxInactive is the amount of time after which an entry is stale.
const maxInactive = 3 * time.Second

// EnableAndRememberFile enables BBR on |tc| and remembers the associated
// *os.File for later, when we'll need it to access BBR stats.
func EnableAndRememberFile(tc *net.TCPConn) error {
	// Implementation note: according to a 2013 message on golang-nuts [1], the
	// code that follows is broken on Unix because calling File() makes the socket
	// blocking so causing Go to use more threads and, additionally, "timer wheel
	// inside net package never fires". However, an April, 19 2018 commit
	// on src/net/tcpsock.go apparently has removed such restriction and so now
	// (i.e. since go1.11beta1) it's safe to use the code below [2, 3].
	//
	// [1] https://grokbase.com/t/gg/golang-nuts/1349whs82r
	//
	// [2] https://github.com/golang/go/commit/60e3ebb9cba
	//
	// [3] https://github.com/golang/go/issues/24942
	//
	// TODO(bassosimone): Should we require builds using the latest version
	// of Go? Warn for earlier versions? Or is this not that big a deal?
	fp, err := tc.File()
	if err != nil {
		return err
	}
	err = enableBBR(fp)
	if err != nil {
		// Do not leak the file. It is important to stress that golang returns
		// a dup()ed descriptor along with |fp|, hence the |tc| connection isn't
		// closed after we've closed |fp|.
		fp.Close()
		return err
	}
	curTime := time.Now()
	key := makekey(tc)
	mutex.Lock()
	defer mutex.Unlock()
	if curTime.Sub(lastCheck) > checkInterval {
		lastCheck = curTime
		// Note: in Golang it's safe to remove elements from the map while
		// iterating it. See <https://github.com/golang/go/issues/9926>.
		for key, entry := range cache {
			if curTime.Sub(entry.Stamp) > maxInactive {
				entry.Fp.Close()
				delete(cache, key)
			}
		}
	}
	cache[key] = entry{
		Fp:    fp, // This takes ownership of fp
		Stamp: curTime,
	}
	return nil
}

// GetAndForgetFile returns the *os.File bound to |conn| that was previously
// saved with EnableAndRememberFile, or nil if no file was found. Note that you
// are given ownership of the returned file pointer. As the name implies, the
// *os.File is removed from the cache by this operation.
func GetAndForgetFile(conn net.Conn) *os.File {
	key := makekey(conn)
	mutex.Lock()
	defer mutex.Unlock()
	entry, found := cache[key]
	if !found {
		return nil
	}
	delete(cache, key)
	return entry.Fp // Pass ownership to caller
}

// GetBandwidthAndRTT obtains BBR info from |fp|. The returned values are
// the max-bandwidth in bytes/s and the min-rtt in microseconds.
func GetBandwidthAndRTT(fp *os.File) (float64, float64, error) {
	// Implementation note: for simplicity I have decided to use float64 here
	// rather than uint64, mainly because the proper C type to use AFAICT (and
	// I may be wrong here) changes between 32 and 64 bit. That is, it is not
	// clear to me how to use a 64 bit integer (which I what I would have used
	// by default) on a 32 bit system. So let's use float64.
	return getBandwidthAndRTT(fp)
}
