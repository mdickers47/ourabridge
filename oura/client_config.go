package oura

import (
	"errors"
	"log"
	"net/url"
	"os"

	"github.com/mdickers47/ourabridge/jdump"
	"golang.org/x/oauth2"
)

type ClientConfig struct {
	MyBaseURL      string
	ApiBaseURL     string
	LocalDataLog   string
	GraphiteServer string
	GraphitePrefix string
	OauthConfig    oauth2.Config
	// {
	//   RedirectURL  string
	//   ClientID     string
	//   ClientSecret string
	//   Scopes       []string
	//   Endpoint {
	//     AuthURL string
	//     TokenURL string
	//   }
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


func GetClientConfig(fname string) ClientConfig {
	cc := ClientConfig{
		MyBaseURL:      "TODO",
		ApiBaseURL:     "https://api.ouraring.com/v2",
		LocalDataLog:   "data.txt",
		GraphiteServer: "",
		GraphitePrefix: "bio.",
		OauthConfig: oauth2.Config{
			RedirectURL:  "TODO",
			ClientID:     "TODO",
			ClientSecret: "TODO",
			Scopes: []string{"email", "personal", "daily", "heartrate",
				"workout", "spo2"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://cloud.ouraring.com/oauth/authorize",
				TokenURL: "https://api.ouraring.com/oauth/token",
			},
		},
	}
	_, err := os.Stat(fname)
	if errors.Is(err, os.ErrNotExist) {
		jdump.DumpJsonOrDie(fname, cc)
		log.Fatalf("edit %s, then try running again", fname)
	}
	jdump.ParseJsonOrDie(fname, &cc)
	return cc
}
