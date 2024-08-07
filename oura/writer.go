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

func StoreObservations(cfg *ClientConfig, src chan Observation) {
	var local    *os.File
	var graphite net.Conn
	var have_local, have_graphite bool
	var err      error

	if len(cfg.LocalDataLog) > 0 {
		local, err = os.OpenFile(cfg.LocalDataLog,
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("can't open log file: %v", err)
		}
		defer local.Close()
		log.Printf("observations logged to local file %s", cfg.LocalDataLog)
		have_local = true
	}

	if len(cfg.GraphiteServer) > 0 {
		graphite, err = net.Dial("tcp", cfg.GraphiteServer)
		if err != nil {
			log.Fatalf("can't connect to graphite server: %s", err)
		}
		log.Printf("connected to graphite line receiver at %s", cfg.GraphiteServer)
		have_graphite = true
	}

	if !(have_local || have_graphite) {
		log.Fatalf("neither local log file nor graphite server is provided")
	}

	for obs := range src {
		line := fmt.Sprintf("%s%s.%s %f %d\n",
			cfg.GraphitePrefix,
			obs.Username,
			obs.Field,
			obs.Value,
			obs.Timestamp.Unix())
		if have_local {
			if _, err = io.WriteString(local, line); err != nil {
				// if we are unable to record the observations, it is best to die
				log.Fatalf("can't write to log file: %v", err)
			}
		}
		if have_graphite {
			if _, err = io.WriteString(graphite, line); err != nil {
				log.Fatalf("can't write to graphite server: %v", err)
			}
		}
	}
}
