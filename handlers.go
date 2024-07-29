package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "<html><head><title>Oura Timeseries Bridge</title></head>")
	io.WriteString(w, "<style>tr:nth-of-type(odd) { background-color: gray; }</style>\n")
	io.WriteString(w, "<body>\n<h2>Current tokens</h2>\n")
	io.WriteString(w, "<table><tr><th>username</th><th>email</th><th>expiration</th><th>last poll</th></tr>\n")
	for _, ut := range userTokens {
		io.WriteString(w,
			fmt.Sprintf("<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
				ut.Name,
				censorEmail(ut.PI.Email),
				ut.Token.Expiry.Format(time.RFC3339),
				ut.LastPoll.Format(time.RFC3339)))
	}
	io.WriteString(w, "</table>\n<h2>Go Oauth yourself</h2>\n")
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
	nonce := make([]byte, 18)
	rand.Read(nonce)
	state := (r.FormValue("username") + ":" +
		base64.URLEncoding.EncodeToString(nonce))
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
	if msg := validateUsername(un, true); msg != "" {
		sendError(w, msg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// exchange the "code" for a "token"
	tok, err := oauthConfig.Exchange(ctx, r.FormValue("code"),
		oauth2.AccessTypeOffline)
	if err != nil {
		sendError(w, fmt.Sprintf("on exchange: %v", err))
		return
	}
	log.Printf("completed code-token exchange for state=%s", cookie.Value)

	// populate personal_info, which also tests that the new token works
	ut := userTokens[un]
	ut.Token = *tok
	err = doOuraDocRequest(oauthConfig, &ut, "personal_info", &ut.PI)
	if err != nil {
		sendError(w, fmt.Sprintf("failed to fetch personal_info: %v", err))
		return
	}

	// this fooled me for a while: all map retrievals and slice operations
	// return a copy of the struct value.  modifying that copy does nothing
	// to the copy in the map/array.  but you can modify the copy and then
	// store it back in the map/array.
	userTokensLock.Lock()
	userTokens[un] = ut
	dumpJsonOrDie(UsersFile, userTokens)
	userTokensLock.Unlock()
	log.Printf("new token expires %v", userTokens[un].Token.Expiry)
	log.Printf("fetched personal_info for %s: ID %s Email %s",
		userTokens[un].Name, userTokens[un].PI.ID, userTokens[un].PI.Email)
	http.Redirect(w, r, "home", http.StatusTemporaryRedirect)
	dailyChan <- userTokens[un]
}

/* WIP

func handleEvent(w http.ResponseWriter, r *http.Request) {

	event := eventNotification{}
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("can't read request body: %s", err)
		return
	}
	err = json.Unmarshal(buf, &event)
	if err != nil {
		log.Printf("can't parse request body: %s", err)
		log.Printf("body was: %s", buf)
		return
	}

}
*/
