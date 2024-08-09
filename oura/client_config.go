package oura

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/mdickers47/ourabridge/jdump"
	"golang.org/x/oauth2"
)

type ClientConfig struct {
	MyBaseURL         string
	ApiBaseURL        string
	LocalDataLog      string
	GraphiteServer    string
	GraphitePrefix    string
	TimeoutSeconds    int
	SubscriptionsFile string
	UserCredsFile     string
	ListenAddr        string
	OauthConfig       oauth2.Config
	// {
	//   RedirectURL  string // ??
	//   ClientID     string
	//   ClientSecret string
	//   Scopes       []string
	//   Endpoint {
	//     AuthURL string
	//     TokenURL string
	//   }
	Reconnect     bool            `json:"-"`
	Verifier      string          `json:"-"`
	UserTokens    UserTokenSet    `json:"-"`
	Subscriptions SubscriptionSet `json:"-"`
}

func validURL(u string) *url.URL {
	v_url, err := url.Parse(u)
	if err != nil {
		log.Fatalf("bogus url value %s: %v", u, err)
	}
	return v_url
}

func (cfg *ClientConfig) MyPath(p string) *url.URL {
	u := validURL(cfg.MyBaseURL)
	u.Path += p
	return u
}

func (cfg *ClientConfig) OuraPath(p string) *url.URL {
	u := validURL(cfg.ApiBaseURL)
	u.Path += p
	return u
}

func (cfg *ClientConfig) NewContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(),
		time.Duration(cfg.TimeoutSeconds)*time.Second)
}

func LoadClientConfig(fname string) ClientConfig {
	cc := ClientConfig{
		MyBaseURL:         "TODO",
		ApiBaseURL:        "https://api.ouraring.com/v2",
		LocalDataLog:      "data.txt",
		GraphiteServer:    "",
		GraphitePrefix:    "bio.",
		TimeoutSeconds:    10,
		SubscriptionsFile: "subscriptions.json",
		UserCredsFile:     "user_creds.json",
		ListenAddr:        "127.0.0.1:8000",
		OauthConfig: oauth2.Config{
			RedirectURL:  "TODO",
			ClientID:     "TODO",
			ClientSecret: "TODO",
			Scopes: []string{"email", "personal", "daily", "heartrate",
				"workout", "spo2"},
			Endpoint: oauth2.Endpoint{
				AuthURL:       "https://cloud.ouraring.com/oauth/authorize",
				DeviceAuthURL: "unused",
				TokenURL:      "https://api.ouraring.com/oauth/token",
			},
		},
	}
	stat, err := os.Stat(fname)
	if stat.Size() == 0 || errors.Is(err, os.ErrNotExist) {
		jdump.DumpJsonOrDie(fname, cc)
		log.Fatalf("edit %s, then try running again", fname)
	}
	jdump.ParseJsonOrDie(fname, &cc)
	cc.UserTokens = MakeUserTokenSet(cc.UserCredsFile)
	cc.Subscriptions = SubscriptionSet{File: cc.SubscriptionsFile}
	cc.Subscriptions.Subs = make([]subResponse, 0, 8)
	stat, err = os.Stat(cc.Subscriptions.File)
	if stat.Size() > 0 && err == nil {
		jdump.ParseJsonOrDie(cc.Subscriptions.File, &cc.Subscriptions.Subs)
	} else {
		log.Printf("warning: subscriptions file %s is missing or empty",
			cc.Subscriptions.File)
	}
	return cc
}

func (cfg *ClientConfig) OauthClient(user string) (*http.Client,
	context.CancelFunc) {
	// We can't use the library supplied Client() because it has a cool
	// behavior where it refreshes the token silently and gives you no
	// way to see or save the replacement token.  It is very hard to
	// imagine the case where this is useful.  All your oauth grants
	// will be lost when the process exits.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	tok := cfg.UserTokens.GetOauthToken(user)
	if tok == nil {
		log.Printf("tried to create oauth client for missing user %s", user)
		return nil, nil
	}
	ts := NonBrokenTokenSource{
		tokensource: cfg.OauthConfig.TokenSource(ctx, tok),
		username:    user,
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
	nbts.tokenset.UpdateOauthToken(nbts.username, *tok)
	return tok, err
}
