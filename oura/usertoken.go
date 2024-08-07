package oura

import (
	"context"
	"golang.org/x/oauth2"
	"net/http"
	"time"
	"log"

	"github.com/mdickers47/ourabridge/jdump"
)

type UserToken struct {
	Name          string
	PI            personalInfo
	OauthToken    oauth2.Token
	LastPoll      time.Time
}

func (ut *UserToken) CensorToken() string {
	tok := "none"
	if len(ut.OauthToken.AccessToken) >= 5 {
		tok = ut.OauthToken.AccessToken[:5]
	}
	return tok
}

func (ut *UserToken) HttpClient(cfg *ClientConfig) (*http.Client,
	context.CancelFunc) {
	// We can't use the library supplied Client() because it has a cool
	// behavior where it refreshes the token silently and gives you no
	// way to see or save the replacement token.  It is very hard to
	// imagine the case where this is useful.  All your oauth grants
	// will be lost when the process exits.
	log.Printf("somebody asked me for a HttpClient, my OauthToken is %s",
		ut.CensorToken())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ts := NonBrokenTokenSource{
		tokensource: cfg.OauthConfig.TokenSource(ctx, &ut.OauthToken),
		username:    ut.Name,
		tokenset:    &cfg.UserTokens,
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
	tokenset    *UserTokenSet
}

func (nbts NonBrokenTokenSource) Token() (*oauth2.Token, error) {
	tok, err := nbts.tokensource.Token()
	if err != nil {
		return nil, err
	}
	if err == nil && tok == nil {
		log.Printf("somehow tok is nil and error is nil")
	}
	ut := nbts.tokenset.FindByName(nbts.username)
	if tok.AccessToken != ut.OauthToken.AccessToken {
		log.Printf("caught an oauth token refresh for %s, new AccessToken is %s",
			ut.Name, tok.AccessToken[:5])
		// this is a failsafe until I understand why we are losing the new token
		jdump.DumpJsonOrDie("debug_new_token.json", tok)
		ut.OauthToken = *tok
		nbts.tokenset.Replace(nbts.username, *ut)
	}
	return tok, err
}			

