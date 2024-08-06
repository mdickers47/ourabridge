package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func handleHome(w http.ResponseWriter, r *http.Request) {

	// non-goal: writing a whole new package for html generation
	thing := func(tag string, val string) {
		_, err := io.WriteString(w, fmt.Sprintf("<%s>%s</%s>", tag, val, tag))
		if err != nil {
			log.Printf("can't write to http response: %s", err)
		}
	}

	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "<html><head><title>Oura Timeseries Bridge</title></head>")
	io.WriteString(w, "<style>tr:nth-of-type(odd) { background-color: gray; }</style>\n")
	io.WriteString(w, "<body>\n")
	thing("h2", "Current tokens")
	io.WriteString(w, "<table><tr><th>username</th><th>email</th><th colspan=\"2\">expiration</th><th colspan=\"2\">last poll</th></tr>\n")

	dur := func(t time.Time) string {
		return fmt.Sprintf("%s",
			time.Now().Round(time.Minute).Sub(t).Round(time.Minute))
		// put that in your v8 and smoke it
	}

	for _, ut := range UserTokens.Tokens {
		io.WriteString(w, "<tr>")
		thing("td", ut.Name)
		thing("td", censorEmail(ut.PI.Email))
		thing("td", ut.OauthToken.Expiry.Format(time.RFC3339))
		thing("td", dur(ut.OauthToken.Expiry))
		thing("td", ut.LastPoll.Format(time.RFC3339))
		thing("td", dur(ut.LastPoll))
		io.WriteString(w, "</tr>")
	}
	io.WriteString(w, "</table>")
	thing("p", "Current time: "+time.Now().Format(time.RFC3339))
	thing("h2", "Go Oauth yourself")
	io.WriteString(w, "<form action=\"/newlogin\">"+
		"<label for=\"username\">Choose a username</label>"+
		"<br/><input type=\"text\" name=\"username\">"+
		"<br/><input type=\"submit\" value=\"Go\"></form>"+
		"</body></html>\n")
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	// save the requested username in the opaque "state" string that
	// oauth2 provides.  note that you are also supposed to put a nonce
	// in "state" for some obscure csrf reason.
	un := strings.ToLower(r.FormValue("username"))
	if msg := validateUsername(un, false); msg != "" {
		sendError(w, msg)
		return
	}
	state := (r.FormValue("username") + ":" + oura.RandomString())
	http.SetCookie(w, &http.Cookie{
		Name:    "oauthstate",
		Value:   state,
		Expires: time.Now().Add(30 * time.Minute),
	})
	url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleAuthCode(w http.ResponseWriter, r *http.Request) {

	// validate the nonce which we get back as both a cookie and a form value
	cookie, _ := r.Cookie("oauthstate")
	if cookie != nil && r.FormValue("state") != cookie.Value {
		msg := fmt.Sprintf("state nonce mismatch: cookie %v formvalue %v\n",
			cookie.Value, r.FormValue("state"))
		sendError(w, msg)
		return
	}
	log.Printf("valid code callback for state=%s", cookie.Value)

	un := strings.Split(cookie.Value, ":")[0]
	log.Printf("claiming username: %s", un)
	if msg := validateUsername(un, true); msg != "" {
		log.Printf("could not validate/claim username: %s", msg)
		sendError(w, msg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log.Printf("starting exchange for username=%s", un)
	// exchange the "code" for a "token"
	tok, err := oauthConfig.Exchange(ctx, r.FormValue("code"),
		oauth2.AccessTypeOffline)
	if err != nil {
		sendError(w, fmt.Sprintf("could not exchange code: %v", err))
		return
	}
	log.Printf("completed code-token exchange for username=%s", un)

	// populate personal_info, which also tests that the new token works
	ut := UserTokens.FindByName(un)
	ut.OauthToken = *tok
	log.Printf("ut going in is %v", ut)
	log.Printf("the OauthToken of ut going in is %v", ut.OauthToken)
	err = doOuraDocRequest(ut, "personal_info", &ut.PI)
	if err != nil {
		sendError(w, fmt.Sprintf("failed to fetch personal_info: %v", err))
		return
	}

	// this fooled me for a while: all map retrievals and slice operations
	// return a copy of the struct value.  modifying that copy does nothing
	// to the copy in the map/array.  but you can modify the copy and then
	// store it back in the map/array.
	UserTokens.Replace(un, *ut)
	log.Printf("new token expires %v", ut.OauthToken.Expiry)
	log.Printf("fetched personal_info for %s: ID %s Email %s",
		ut.Name, ut.PI.ID, ut.PI.Email)
	http.Redirect(w, r, "home", http.StatusTemporaryRedirect)
	dailyChan <- *ut
}

func handleEvent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		// this is how they verify that you are listening at subscription
		// time
		log.Printf("received subscription verifier request: %s", r.URL.String())
		if r.FormValue("verification_token") != SubscriptionToken {
			log.Printf("subscription callback token: got %s, expected %s",
				r.FormValue("verification_token"), SubscriptionToken)
			//w.WriteHeader(http.StatusBadRequest)
			//return
		}
		// our proof of worthiness is to take the string out of the query
		// parameter, encode it in a json container, and send it back
		buf, err := json.Marshal(struct{ Challenge string }{
			Challenge: r.FormValue("challenge"),
		})
		if err != nil {
			msg := fmt.Sprintf("failed to json-encode challenge: %v", err)
			log.Println(msg)
			w.WriteHeader(http.StatusBadRequest)
			writeLogErr(w, msg)
			return
		}
		log.Printf("returning challenge: %s", buf)
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(buf)
		if err != nil {
			log.Printf("can't write to output stream: %v", err)
		}
	case "POST":
		event := eventNotification{}
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			msg := fmt.Sprintf("can't read request body: %s", err)
			log.Printf(msg)
			w.WriteHeader(http.StatusBadRequest)
			writeLogErr(w, msg)
			return
		}
		err = json.Unmarshal(buf, &event)
		if err != nil {
			msg := fmt.Sprintf("can't parse request body: %s", err)
			log.Printf(msg)
			log.Printf("body was: %s", buf)
			w.WriteHeader(http.StatusBadRequest)
			writeLogErr(w, msg)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeLogErr(w, "Thanks Chief!")
		eventChan <- event
	default:
		log.Printf("weird HTTP method: %s", r.Method)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func writeLogErr(w io.Writer, s string) {
	_, err := io.WriteString(w, s)
	if err != nil {
		log.Printf("write error: %v", err)
	}
}
