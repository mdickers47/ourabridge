package main

import (
	//"crypto/tls"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mdickers47/ourabridge/oura"
)

var QuietStart = flag.Bool("quietstart", false,
	"Don't send any requests to Oura API at startup")

/*
var TlsKey = flag.String("tlspem", "privkey.pem",
	"Path to private key PEM file")
var TlsCert = flag.String("tlscert", "cert.pem",
	"Path to certificate PEM file")
*/

var ClientFile = flag.String("clientsecrets", "client_creds.json",
	"Path to JSON file containing oauth2 ClientID and ClientSecret")

// this is a singleton object that basically all of the code will want
// to access, so a global variable is no worse than passing a
// reference through every single method, and saves a lot of mess.
var Cfg *oura.ClientConfig

func sendError(w http.ResponseWriter, msg string) {
	log.Println(msg)
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, msg)
}

/*
func loadCertOrDie() *tls.Config {
	cert, err := tls.LoadX509KeyPair(*TlsCert, *TlsKey)
	if err != nil {
		log.Fatalf("can't load tls certificate: %v", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}
*/

func censorEmail(email string) string {
	fields := strings.Split(email, "@")
	if len(fields) < 2 {
		// whatever you gave us was probably not an email address, but let's
		// not panic
		return "*****"
	}
	return fmt.Sprintf("%s***%s@%s", fields[0][:1],
		fields[0][len(fields[0])-1:], fields[1])
}

func validateUsername(n string, claim bool) string {
	if len(n) < 3 || len(n) > 12 {
		return "username must have 3 to 12 characters"
	} else if strings.Index(n, ".") >= 0 {
		return "username cannot contain ."
	}
	if Cfg.UserTokens.NameIsTaken(n) {
		return "username is taken"
	}
	if claim {
		// it's Replacing nothing but that's fine
		Cfg.UserTokens.Replace(n, oura.UserToken{Name: n})
	}
	return ""
}

func startHttp(addr string, mux http.ServeMux) *http.Server {
	// a corny way to get http.Server to log requests
	logger := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			log.Printf("HTTP %s %s %s\n", r.RemoteAddr, r.Method, r.URL)
			mux.ServeHTTP(w, r)
		},
	)
	srv := &http.Server{Addr: addr, Handler: logger}
	go func() {
		err := srv.ListenAndServe()
		log.Printf("HTTP server exited: %s", err)
	}()
	log.Printf("HTTP server started on %s", addr)
	// the only reason we want a handle to srv is so that we can call
	// srv.Shutdown()
	return srv
}

func poll(sink chan<- string) {
	i := 0
	for {
		<-time.After(time.Minute * 60)
		for _, ut := range Cfg.UserTokens.CopyUserTokens() {
			sink <- ut.Name
		}
		if i += 1; i%10 == 0 {
			oura.ValidateSubscriptions(Cfg)
		}
	}
}

func sigHandler(source <-chan os.Signal, sink chan<- string) {
	for sig := range source {
		switch sig {
		case syscall.SIGHUP:
			log.Printf("received SIGHUP, rereading config files and reopening logs")
			*Cfg = oura.LoadClientConfig(*ClientFile)
			Cfg.Reconnect = true
		case syscall.SIGUSR1:
			log.Printf("received SIGUSR1, re-polling documents and subscriptions")
			oura.ValidateSubscriptions(Cfg)
			for _, ut := range Cfg.UserTokens.CopyUserTokens() {
				sink <- ut.Name
			}
		}
	}
}

func main() {
	flag.Parse()
	log.Println("=======> start")

	// set up oauth, which means retrieving the client secrets and the
	// cached user tokens.
	cc := oura.LoadClientConfig(*ClientFile)
	Cfg = &cc

	// observationChan is the final destination of the processed api
	// responses, after they have been turned into (metric, value,
	// timestamp) tuples.
	observationChan := make(chan oura.Observation, 100)
	go oura.StoreObservations(Cfg, observationChan)

	// create a channel where anyone can put a username and it will get
	// all its documents re-searched.
	pollChan := make(chan string)
	go func() {
		for p := range pollChan {
			oura.SearchAll(Cfg, p, observationChan)
		}
	}()

	// webhook events are nothing more than notifications that a new
	// document is ready.  the webhook callback handler will chuck the
	// incoming document IDs into a channel, and they will be fetched
	// serially.
	eventChan := make(chan oura.EventNotification)
	go func() {
		for e := range eventChan {
			oura.ProcessEvent(Cfg, e, observationChan)
		}
	}()

	mux := http.NewServeMux()
	/* bizarre mystery: with a proxy_pass match on /tsbridge/, nginx
	   intercepts the path /tsbridge/login and sends the client a
	   redirect to /login, which makes it unusable.  it only does this
	   for this one specific word, so far as I have discovered.  there
	   is no mention of this that Google can find.
	*/
	mux.HandleFunc("/", handleHome)
	mux.HandleFunc("/home", handleHome)
	mux.HandleFunc("/newlogin", handleLogin)
	mux.HandleFunc("/code", func(w http.ResponseWriter, r *http.Request) {
		handleAuthCode(w, r, pollChan)
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		handleEvent(w, r, eventChan)
	})
	srv := startHttp(Cfg.ListenAddr, *mux)

	if !*QuietStart {
		// start subscriptions -- note that we have to have ListenAndServe()
		// going first, because of callbacks!
		oura.ValidateSubscriptions(Cfg)
		// refresh the daily documents
		for _, ut := range Cfg.UserTokens.CopyUserTokens() {
			pollChan <- ut.Name
		}
	}

	// periodically run document searches and refresh subscriptions
	go poll(pollChan)

	// handle SIGHUP and SIGUSR1
	sigChan := make(chan os.Signal, 1)
	go sigHandler(sigChan, pollChan)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGUSR1)

	// WAIT HERE for a SIGINT or SIGTERM
	bye := make(chan os.Signal, 1)
	signal.Notify(bye, syscall.SIGINT, syscall.SIGTERM)
	<-bye

	// graceful stop of HTTP server..?
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("failed to stop HTTP server: %s", err)
	}
	log.Println("=======> See You Space Cowboy")
}
