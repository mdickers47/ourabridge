package main

import (
	"context"
	"golang.org/x/oauth2"
	"net/http"
	"sync"
	"time"
	"log"
)

type userToken struct {
	Name          string
	PI            personalInfo
	OauthToken    oauth2.Token
	Subscriptions []subResponse
	LastPoll      time.Time
}

func (ut *userToken) GetSubscription(data_type string,
	event_type string) (int, *subResponse) {
	for i, s := range ut.Subscriptions {
		if s.Data_type == data_type && s.Event_type == event_type {
			return i, &s
		}
	}
	return -1, nil
}

func (ut *userToken) ReplaceSubscription(new *subResponse) {
	idx, _ := ut.GetSubscription(new.Data_type, new.Event_type)
	if idx >= 0 {
		// this oddity is claimed to be the idiomatic Go way to delete something
		// from a slice
		ut.Subscriptions = append(ut.Subscriptions[:idx],
			ut.Subscriptions[idx+1:]...)
	}
	ut.Subscriptions = append(ut.Subscriptions, *new)
}

func (ut *userToken) HttpClient() (*http.Client, context.CancelFunc) {
	// We can't use the library supplied Client() because it has a cool
	// behavior where it refreshes the token silently and gives you no
	// way to see or save the replacement token.  It is very hard to
	// imagine the case where this is useful.  All your oauth grants
	// will be lost when the process exits.
	log.Printf("somebody asked me for a HttpClient, my OauthToken is %v",
		ut.OauthToken.AccessToken[:6])
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	ts := NonBrokenTokenSource{
			tokensource: oauthConfig.TokenSource(ctx, &ut.OauthToken),
			username:    ut.Name,
	}		
	c := oauth2.NewClient(ctx, ts)
	return c, cancel
}

// the purpose of NonBrokenTokenSource is to replace the TokenSource
// that would have been created and used under the hood inside the
// oauth2.Config.Client() access point.  That implementation is
// useless because it does not save or expose the new token anywhere
// when a refresh happens.  See here (solution by dnesting):
// https://github.com/golang/oauth2/issues/84
type NonBrokenTokenSource struct {
	tokensource oauth2.TokenSource
	username    string
}

func (nbts NonBrokenTokenSource) Token() (*oauth2.Token, error) {
	tok, err := nbts.tokensource.Token()
	if err != nil {
		return nil, err
	}
	if err == nil && tok == nil {
		log.Printf("somehow tok is nil and error is nil")
	}
	ut := UserTokens.FindByName(nbts.username)
	if tok.AccessToken != ut.OauthToken.AccessToken {
		log.Printf("caught an oauth token refresh for %s, new AccessToken is %s",
			ut.Name, tok.AccessToken[:6])
		// this is a failsafe until I understand why we are losing the new token
		f := "debug_new_token.json"
		dumpJsonOrDie(&f, tok)
		ut.OauthToken = *tok
		UserTokens.Replace(nbts.username, *ut)
	}
	return tok, err
}			

// a userTokenSet is a container for a bunch of userTokens that you
// should only access through its methods, in order to be thread safe.
type userTokenSet struct {
	Tokens map[string]userToken
	Lock   sync.Mutex
}

// this is the global data structure that we will use anywhere it is
// convenient lol
var UserTokens userTokenSet

func (set *userTokenSet) FindById(id string) *userToken {
	for _, i := range set.Tokens {
		if i.PI.ID == id {
			return &i
		}
	}
	return nil
}

func (set *userTokenSet) FindByName(name string) *userToken {
	// probably overkill but maybe this implementation changes
	// some day
	ut, ok := set.Tokens[name]
	if !ok {
		return nil
	} else {
		return &ut
	}
}

func (set *userTokenSet) Replace(name string, ut userToken) {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	set.Tokens[name] = ut
	set.Save()
	log.Printf("updated and saved token for %s: %s",
		name, ut.OauthToken.AccessToken[:6])
}

func (set *userTokenSet) Save() {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	dumpJsonOrDie(UsersFile, set.Tokens)
}

func (set *userTokenSet) CopyUserTokens() []userToken {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	uts := make([]userToken, 0, len(set.Tokens))
	for _, value := range set.Tokens {
		uts = append(uts, value)
	}
	return uts
}
