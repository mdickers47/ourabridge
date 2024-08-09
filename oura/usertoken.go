package oura

import (
	"golang.org/x/oauth2"
	"time"
)

type UserToken struct {
	Name       string
	PI         PersonalInfo
	OauthToken oauth2.Token
	LastUse    time.Time
}

func (ut *UserToken) CensorToken() string {
	tok := "none"
	if len(ut.OauthToken.AccessToken) >= 5 {
		tok = ut.OauthToken.AccessToken[:5]
	}
	return tok
}

