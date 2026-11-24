// Command harness runs the tether core in userspace networking on Linux,
// for testing it against a real control server without a phone:
//
//	harness -control https://... -state ./state -hostname tether-test
//
// It prints events and a status line every -every; SIGUSR1 feeds it a path
// change (alternating two fake interfaces), SIGUSR2 a repeat of the current
// path, so the filter and the rebind behaviour can be watched.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sstrabe/tether/core/tether"
)

type host struct{}

func (host) ApplySettings(s tether.NetworkSettings) error {
	b, _ := json.Marshal(s)
	log.Printf("settings: %s", b)
	return nil
}

func (host) Event(e tether.Event) {
	b, _ := json.Marshal(e)
	log.Printf("event: %s", b)
}

func main() {
	control := flag.String("control", "", "control server URL (https)")
	state := flag.String("state", "./state", "state directory")
	hostname := flag.String("hostname", "tether-harness", "node name")
	every := flag.Duration("every", 30*time.Second, "status interval")
	login := flag.Bool("login", true, "start an interactive login if needed")
	flag.Parse()

	e, err := tether.Start(tether.Config{
		ControlURL:   *control,
		Hostname:     *hostname,
		StateDir:     *state,
		TunFD:        -1,
		AcceptRoutes: true,
		AcceptDNS:    true,
	}, host{})
	if err != nil {
		log.Fatal(err)
	}
	defer e.Stop()
	if *login {
		go func() {
			time.Sleep(3 * time.Second)
			if st := e.Status(); st.State == "NeedsLogin" {
				if err := e.Login(); err != nil {
					log.Printf("login: %v", err)
				}
			}
		}()
	}

	paths := []tether.Path{
		{Satisfied: true, Interface: "eth0", IPv4: true, IPv6: true},
		{Satisfied: true, Interface: "eth1", IPv4: true},
	}
	pi := 0
	e.PathChanged(paths[0])

	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
	tick := time.NewTicker(*every)
	defer tick.Stop()
	for {
		select {
		case s := <-sig:
			switch s {
			case syscall.SIGUSR1:
				pi = 1 - pi
				e.PathChanged(paths[pi])
			case syscall.SIGUSR2:
				e.PathChanged(paths[pi])
			default:
				return
			}
		case <-tick.C:
			st := e.Status()
			online := 0
			for _, p := range st.Peers {
				if p.Online {
					online++
				}
			}
			fmt.Printf("status: state=%s peers=%d online=%d paths=%d/%d heap=%.1fMB sys=%.1fMB health=%v\n",
				st.State, len(st.Peers), online, st.Counters.PathChanges, st.Counters.PathUpdates,
				st.Memory.HeapMB, st.Memory.SysMB, st.Health)
		}
	}
}
