package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var ServerAddr = flag.String("addr", ":8000",
	"Address for listen socket")
var BaseUrl = flag.String("baseurl", "https://oura.singingtree.com",
	"My server name and path prefix")

/*
var TlsKey = flag.String("tlspem", "privkey.pem",
	"Path to private key PEM file")
var TlsCert = flag.String("tlscert", "cert.pem",
	"Path to certificate PEM file")
*/

var ClientFile = flag.String("clientsecrets", "client_creds.json",
	"Path to JSON file containing oauth2 ClientID and ClientSecret")
var UsersFile = flag.String("usertokens", "user_creds.json",
	"Path to JSON file containing list of user tokens")
var GraphitePrefix = flag.String("graphiteprefix", "bio.",
	"Prefix to Graphite data points")
var LogFile = flag.String("logfile", "data.txt",
	"Local log file for observations")

type clientSecrets struct {
	ClientID     string
	ClientSecret string
}

var oauthConfig *oauth2.Config
var dailyChan chan userToken
var eventChan chan eventNotification

func parseJsonOrDie(f *string, dest any) {
	bytes, err := os.ReadFile(*f)
	if err != nil {
		log.Fatalf("can't read json file: %v", err)
	}
	err = json.Unmarshal(bytes, dest)
	if err != nil {
		log.Fatalf("can't parse json file: %v", err)
	}
}

func dumpJsonOrDie(f *string, obj any) {
	buf, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		log.Fatalf("error encoding json: %v", err)
	}
	err = os.WriteFile(*f, buf, 0600)
	if err != nil {
		log.Fatalf("error saving json file %v: %v", f, err)
	}
}

func sendError(w http.ResponseWriter, msg string) {
	log.Println(msg)
	w.Header().Add("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	io.WriteString(w, msg)
}

func loadCertOrDie() *tls.Config {
	cert, err := tls.LoadX509KeyPair(*TlsCert, *TlsKey)
	if err != nil {
		log.Fatalf("can't load tls certificate: %v", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

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
	if UserTokens.FindByName(n) != nil {
		return "username is taken"
	}
	if claim {
		// it's Replacing nothing but that's fine
		UserTokens.Replace(n, userToken{Name: n})
	}
	return ""
}

func processResponse[T ouraDoc](err error, doclist []T, ut userToken) {
	if err != nil {
		log.Println(err)
	}
	for _, doc := range doclist {
		// I tried to use document timestamps to avoid saving duplicate
		// observations, but it doesn't work without getting complicated,
		// because documents arrive with back-dated timestamps.
		SendDoc(doc, ut.Name)
	}
}

func fetchDailiesOnDemand() {
	for ut := range dailyChan {
		dr := readinessResponse{}
		err := doOuraDocRequest(&ut, "daily_readiness", &dr)
		processResponse(err, dr.Data, ut)
		da := activityResponse{}
		err = doOuraDocRequest(&ut, "daily_activity", &da)
		processResponse(err, da.Data, ut)
		ds := sleepResponse{}
		err = doOuraDocRequest(&ut, "daily_sleep", &ds)
		processResponse(err, ds.Data, ut)
		dp := sleepPeriodResponse{}
		err = doOuraDocRequest(&ut, "sleep", &dp)
		processResponse(err, dp.Data, ut)
		hr := heartrateResponse{}
		err = doOuraDocRequest(&ut, "heartrate", &hr)
		processResponse(err, hr.Data, ut)
		ut.LastPoll = time.Now()
		UserTokens.Replace(ut.Name, ut)
	}
}

func pollDailies(die chan bool) {
	for {
		select {
		case <-time.After(time.Minute * 60):
			// take a snapshot of UserTokens, and chuck them all into the
			// daily-fetch hopper
			for _, ut := range UserTokens.CopyUserTokens() {
				dailyChan <- ut
			}
		case <-die:
			return
		}
	}
}

func processWebhookEvents(incoming chan eventNotification) {
	for event := range incoming {
		ut := UserTokens.FindById(event.User_id)
		if ut == nil {
			log.Printf("webhook notification for mystery user: %s", event.User_id)
			continue
		}
		// the other possible Event_type is "delete" and there is nothing we
		// can do with that.
		if event.Event_type == "update" || event.Event_type == "create" {
			switch event.Data_type {
			case "daily_activity":
				da := dailyActivity{}
				err := getOuraDoc(ut, "daily_activity", event.Object_id, &da)
				if err != nil {
					SendDoc(da, ut.Name)
				}
			case "daily_readiness":
				dr := dailyReadiness{}
				err := getOuraDoc(ut, "daily_readiness", event.Object_id, &dr)
				if err != nil {
					SendDoc(dr, ut.Name)
				}
			case "daily_sleep":
				ds := dailySleep{}
				err := getOuraDoc(ut, "daily_sleep", event.Object_id, &ds)
				if err != nil {
					SendDoc(ds, ut.Name)
				}
			case "sleep":
				sp := sleepPeriod{}
				err := getOuraDocById(ut, "sleep", event.Object_id, &sp)
				if err != nil {
					SendDoc(sp, ut.Name)
				}
			default:
				// unhandled types include:
				// tag enhanced_tag workout session daily_spo2 sleep_time
				// rest_mode_period ring_configuration daily_stress
				// daily_cycle_phases
				log.Printf("webhook notification for unhandled type: %s",
					event.Data_type)
			}
		}
	}
}

func renewSubscriptions() {
	// run through all the userTokens and all the data types and create
	// or renew any missing subscriptions.  we stop trying after 3
	// failures because it appears that if you fail too much, you will
	// piss off cloudfront on oura's side and get locked out.
	var err error
	err_count := 0
	for _, ut := range UserTokens.Tokens {
		for _, data_type := range []string{"daily_activity", "daily_readiness",
			"daily_sleep", "sleep"} {
			for _, event_type := range []string{"create", "update"} {
				_, s := ut.GetSubscription(data_type, event_type)
				if s == nil {
					s, err = createOuraSubscription(&ut, event_type, data_type)
					if err != nil {
						log.Printf("can't create subscription %s/%s/%s: %v",
							ut.Name, data_type, event_type, err)
						if err_count += 1; err_count > 2 {
							log.Printf("giving up on subscriptions")
							return
						}
					} else {
						ut.ReplaceSubscription(s)
						log.Printf("subscribed to %s/%s/%s expiring %v",
							ut.Name, data_type, event_type, s.Expiration_time)
					}
				} else {
					// there is already a subscription, let's see if it is expiring
					// soon
					lifetime := s.Expiration_time.Sub(time.Now())
					if lifetime < 2*time.Hour {
						err = renewOuraSubscription(oauthConfig, &ut, s)
						if err != nil {
							log.Printf("can't renew subscription %s/%s/%s: %v",
								ut.Name, data_type, event_type, err)
							if err_count += 1; err_count > 2 {
								log.Printf("giving up on subscriptions")
								return
							}
						} else {
							ut.ReplaceSubscription(s)
							log.Printf("renewed %s/%s/%s until %v",
								ut.Name, data_type, event_type, s.Expiration_time)
						}
					}
				}
			}
		}
	}
}

func main() {
	flag.Parse()
	log.Println("=======> start")

	// start a bunch of workers

	observationChan = make(chan observation, 12)
	go storeObservations()
	dailyChan = make(chan userToken, 3)
	go fetchDailiesOnDemand()
	killPollerChan := make(chan bool)
	go pollDailies(killPollerChan)
	eventChan := make(chan eventNotification)
	go processWebhookEvents(eventChan)

	// set up oauth, which means retrieving the client secrets and the
	// cached user tokens.

	var cs clientSecrets
	parseJsonOrDie(ClientFile, &cs)
	oauthConfig = &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%v%v", *BaseUrl, "/code"),
		ClientID:     cs.ClientID,
		ClientSecret: cs.ClientSecret,
		Scopes: []string{"email", "personal", "daily", "heartrate",
			"workout", "spo2"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://cloud.ouraring.com/oauth/authorize",
			TokenURL: "https://api.ouraring.com/oauth/token",
		},
	}

	parseJsonOrDie(UsersFile, &UserTokens.Tokens)

	// refresh the daily documents at startup
	for _, ut := range UserTokens.Tokens {
		dailyChan <- ut
	}

	// start subscriptions

	/*
		listen, err := tls.Listen("tcp", *ServerAddr, loadCertOrDie())
		if err != nil {
			log.Fatalf("can't start listener: %v", err)
		}
	*/

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
	mux.HandleFunc("/code", handleAuthCode)
	mux.HandleFunc("/event", handleEvent)

	logger := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("HTTP %s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		mux.ServeHTTP(w, r)
	})
	bye := make(chan bool)
	go func() {
		err := http.ListenAndServe(*ServerAddr, logger)
		log.Println(err)
		bye <- true
	}()

	log.Printf("http server is running on %s", *ServerAddr)

	// start subscriptions -- note that we have to have ListenAndServe()
	// going first, because of callbacks!
	renewSubscriptions()

	<-bye
	killPollerChan <- true
	log.Println("=======> See You Space Cowboy")
}
