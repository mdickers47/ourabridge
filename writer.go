package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

type observation struct {
	Timestamp time.Time
	Username  string
	Field     string
	Value     float32
}

var observationChan chan observation
var GraphiteSrv = flag.String("graphite", "", "Address of graphite server")

func storeObservations() {
	local, err := os.OpenFile(*LogFile,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("can't open log file: %v", err)
	}
	defer local.Close()

	var graphite net.Conn
	if len(*GraphiteSrv) > 0 {
		graphite, err = net.Dial("tcp", *GraphiteSrv)
		if err != nil {
			log.Fatalf("can't connect to graphite server: %s", err)
		}
		log.Printf("connected to graphite line receiver at %s", *GraphiteSrv)
	}

	for obs := range observationChan {
		line := fmt.Sprintf("%s%s.%s %f %d\n",
			*GraphitePrefix,
			obs.Username,
			obs.Field,
			obs.Value,
			obs.Timestamp.Unix())
		log.Print(line)
		_, err = io.WriteString(local, line)
		if err != nil {
			// if we are unable to record the observations, it is best to die
			log.Fatalf("can't write to log file: %v", err)
		}
		if graphite != nil {
			_, err = io.WriteString(graphite, line)
			if err != nil {
				log.Fatalf("can't write to graphite server: %v", err)
			}
		}
	}
}
