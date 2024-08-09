package oura

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

type Observation struct {
	Timestamp time.Time
	Username  string
	Field     string
	Value     float32
}

// StoreObservations uses the LocalDataLog, GraphiteServer, and
// GraphitePrefix entries in cfg.  It tries to os.OpenFile the path in
// cfg.LocalDataLog, and tries to net.Dial the address in
// cfg.GraphiteServer.  It closes and reopens them both every 15
// minutes.  When there is a write error to the local file, or when
// cfg.Reconnect == true, it tries to reopen both immediately.  When
// there is a write error to graphite, it lets the 15-minly reconnect
// try again.  If at any time neither LocalLogFile nor GraphiteServer
// is working, then the process dies with a log.Fatalf.

func StoreObservations(cfg *ClientConfig, src chan Observation) {
	var local *os.File
	var graphite net.Conn
	var have_local, have_graphite bool
	var err error
	var next_reconnect time.Time

	close := func() {
		if have_local {
			local.Close()
			have_local = false
		}
		if have_graphite {
			graphite.Close()
			have_graphite = false
		}
	}

	reconnect := func() {
		if len(cfg.LocalDataLog) > 0 {
			local, err = os.OpenFile(cfg.LocalDataLog,
				os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("can't open log file: %s", err)
			} else {
				log.Printf("opened local log file %s", cfg.LocalDataLog)
				have_local = true
			}
		}
		if len(cfg.GraphiteServer) > 0 {
			graphite, err = net.Dial("tcp", cfg.GraphiteServer)
			if err != nil {
				log.Printf("can't connect to graphite server: %s", err)
			} else {
				log.Printf("connected to graphite receiver at %s",
					cfg.GraphiteServer)
				have_graphite = true
			}
		}

		if !(have_local || have_graphite) {
			log.Fatalf("neither local log file nor graphite server is working")
		}
	}

	defer close()
	cfg.Reconnect = true
	for obs := range src {
		if cfg.Reconnect || time.Now().After(next_reconnect) {
			close()
			reconnect()
			cfg.Reconnect = false
			next_reconnect = time.Now().Add(15 * time.Minute)
		}
		line := fmt.Sprintf("%s%s.%s %f %d\n",
			cfg.GraphitePrefix,
			obs.Username,
			obs.Field,
			obs.Value,
			obs.Timestamp.Unix())
		if have_local {
			if _, err = io.WriteString(local, line); err != nil {
				// if we are unable to record the observations, it is best to die
				log.Printf("failed write to log file: %s", err)
				local.Close()
				have_local = false
				// it is a local file, so try to reopen it immediately
				cfg.Reconnect = true
			}
		}
		if have_graphite {
			if _, err = io.WriteString(graphite, line); err != nil {
				log.Printf("failed write to graphite server: %s", err)
				graphite.Close()
				have_graphite = false
				// we will try again at the next reconnect interval, rather
				// than wait for a probably-hanging TCP connect attempt over
				// and over
			}
		}
	}
}
