// Command libtether is the C bridge the iOS app and extension link
// (go build -buildmode=c-archive). One engine per process. Strings returned
// to C are malloc'd; free them with TetherFree.
package main

/*
#include <stdlib.h>

typedef void (*tether_event_cb)(const char *json, void *ctx);
typedef int (*tether_apply_cb)(const char *json, void *ctx);

static void tether_call_event(tether_event_cb cb, const char *json, void *ctx) { cb(json, ctx); }
static int tether_call_apply(tether_apply_cb cb, const char *json, void *ctx) { return cb(json, ctx); }
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/sstrabe/tether/core/tether"
)

var (
	mu      sync.Mutex
	eng     *tether.Engine
	eventCB C.tether_event_cb
	applyCB C.tether_apply_cb
	cbCtx   unsafe.Pointer
)

type cHost struct{}

func (cHost) ApplySettings(s tether.NetworkSettings) error {
	mu.Lock()
	cb, ctx := applyCB, cbCtx
	mu.Unlock()
	if cb == nil {
		return errors.New("no apply callback")
	}
	b, _ := json.Marshal(s)
	cs := C.CString(string(b))
	defer C.free(unsafe.Pointer(cs))
	if rc := C.tether_call_apply(cb, cs, ctx); rc != 0 {
		return fmt.Errorf("applying network settings failed (%d)", int(rc))
	}
	return nil
}

func (cHost) Event(ev tether.Event) {
	mu.Lock()
	cb, ctx := eventCB, cbCtx
	mu.Unlock()
	if cb == nil {
		return
	}
	b, _ := json.Marshal(ev)
	cs := C.CString(string(b))
	defer C.free(unsafe.Pointer(cs))
	C.tether_call_event(cb, cs, ctx)
}

func cerr(err error) *C.char {
	if err == nil {
		return nil
	}
	return C.CString(err.Error())
}

//export TetherSetCallbacks
func TetherSetCallbacks(event C.tether_event_cb, apply C.tether_apply_cb, ctx unsafe.Pointer) {
	mu.Lock()
	defer mu.Unlock()
	eventCB, applyCB, cbCtx = event, apply, ctx
}

// TetherStart starts the engine with a JSON tether.Config. Returns NULL or
// an error message.
//
//export TetherStart
func TetherStart(configJSON *C.char) *C.char {
	var cfg tether.Config
	if err := json.Unmarshal([]byte(C.GoString(configJSON)), &cfg); err != nil {
		return cerr(fmt.Errorf("config: %w", err))
	}
	mu.Lock()
	running := eng != nil
	mu.Unlock()
	if running {
		return cerr(errors.New("already running"))
	}
	e, err := tether.Start(cfg, cHost{})
	if err != nil {
		return cerr(err)
	}
	mu.Lock()
	eng = e
	mu.Unlock()
	return nil
}

func current() *tether.Engine {
	mu.Lock()
	defer mu.Unlock()
	return eng
}

//export TetherStop
func TetherStop() {
	mu.Lock()
	e := eng
	eng = nil
	mu.Unlock()
	if e != nil {
		e.Stop()
	}
}

//export TetherLogin
func TetherLogin() *C.char {
	e := current()
	if e == nil {
		return cerr(errors.New("not running"))
	}
	return cerr(e.Login())
}

//export TetherLogout
func TetherLogout() *C.char {
	e := current()
	if e == nil {
		return cerr(errors.New("not running"))
	}
	return cerr(e.Logout())
}

// TetherPathChanged takes a JSON tether.Path from NWPathMonitor.
//
//export TetherPathChanged
func TetherPathChanged(pathJSON *C.char) {
	e := current()
	if e == nil {
		return
	}
	var p tether.Path
	if json.Unmarshal([]byte(C.GoString(pathJSON)), &p) == nil {
		e.PathChanged(p)
	}
}

//export TetherWake
func TetherWake(sleptSeconds C.double) {
	if e := current(); e != nil {
		e.Wake(time.Duration(float64(sleptSeconds) * float64(time.Second)))
	}
}

// TetherStatus returns the JSON tether.Status, or {"state":"NotRunning"}.
//
//export TetherStatus
func TetherStatus() *C.char {
	e := current()
	if e == nil {
		return C.CString(`{"state":"NotRunning","peers":[],"health":[]}`)
	}
	b, _ := json.Marshal(e.Status())
	return C.CString(string(b))
}

// TetherLogs returns the last n log lines as a JSON array.
//
//export TetherLogs
func TetherLogs(n C.int) *C.char {
	e := current()
	if e == nil {
		return C.CString("[]")
	}
	b, _ := json.Marshal(e.Logs(int(n)))
	return C.CString(string(b))
}

//export TetherFree
func TetherFree(p *C.char) { C.free(unsafe.Pointer(p)) }

func main() {}
