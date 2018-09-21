package main

//#include <stdlib.h>
//#include <string.h>
import "C"

import (
	"context"
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/bassosimone/nuvolari"
)

type controlblock struct {
	Cancel context.CancelFunc
	Ch     chan nuvolari.Event
}

var mutex sync.Mutex
var control *controlblock

//export nuvolari_start_download_
func nuvolari_start_download_(s *C.char) C.int {
	mutex.Lock()
	defer mutex.Unlock()
	if control != nil {
		return C.int(2)
	}
	settings := nuvolari.Settings{}
	blk := controlblock{}
	if s != nil {
		str := C.GoString(s)
		if err := json.Unmarshal([]byte(str), &settings); err != nil {
			return C.int(3)
		}
	}
	client, err := nuvolari.NewClient(settings)
	if err != nil {
		return C.int(4)
	}
	ctx, cancel := context.WithCancel(context.Background())
	blk.Cancel = cancel
	blk.Ch = client.Download(ctx)
	control = &blk
	return C.int(0)
}

//export nuvolari_get_next_event_
func nuvolari_get_next_event_() *C.char {
	mutex.Lock()
	defer mutex.Unlock()
	if control == nil {
		return nil
	}
	ev, more := <-control.Ch
	if !more {
		control = nil
		return nil
	}
	bytes, err := json.Marshal(ev)
	if err != nil {
		control.Cancel()
		for _ = range control.Ch {
			// NOTHING
		}
		return nil
	}
	return C.CString(string(bytes))
}

//export nuvolari_free_event_
func nuvolari_free_event_(s *C.char) {
	C.free(unsafe.Pointer(s))
}

//export nuvolari_stop_
func nuvolari_stop_() {
	mutex.Lock()
	defer mutex.Unlock()
	if control != nil {
		control.Cancel()
	}
}

func main() {
}
