package oura

import (
	"context"
	"errors"
	"log"
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
	cc.UserTokens = UserTokenSet{File: cc.UserCredsFile}
	cc.UserTokens.Tokens = make(map[string]UserToken, 10)
	stat, err = os.Stat(cc.UserTokens.File)
	if stat.Size() > 0 && err == nil {
		jdump.ParseJsonOrDie(cc.UserTokens.File, &cc.UserTokens.Tokens)
	} else {
		log.Printf("warning: user credentials file %s is missing or empty",
			cc.UserTokens.File)
	}
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
