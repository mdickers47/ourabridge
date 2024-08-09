package oura

import (
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mdickers47/ourabridge/jdump"
)

// a userTokenSet is a container for a bunch of UserTokens that you
// should only access through its methods.  It tries to preserve
// itself in json form in the given File.
type UserTokenSet struct {
	tokens map[string]UserToken
	Lock   sync.Mutex
	File   string
}

func MakeUserTokenSet(file string) UserTokenSet {
	s := UserTokenSet{
		File:   file,
		tokens: make(map[string]UserToken, 0),
	}
	stat, err := os.Stat(s.File)
	if stat.Size() > 0 && err == nil {
		jdump.ParseJsonOrDie(s.File, &s.tokens)
	} else {
		log.Printf("warning: user credentials file %s is missing or empty",
			s.File)
	}
	return s
}

func (set *UserTokenSet) saveordie() {
	// this is a private function because we assume you already
	// have set.Lock!
	jdump.DumpJsonOrDie(set.File, set.tokens)
}

func (set *UserTokenSet) findByName(name string) *UserToken {
	// probably overkill but maybe this implementation changes
	// some day
	ut, ok := set.tokens[name]
	if !ok {
		return nil
	} else {
		return &ut
	}
}

func (set *UserTokenSet) Replace(name string, ut UserToken) {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	set.tokens[name] = ut
	set.saveordie()
	log.Printf("updated and saved token for %s: %s", name, ut.CensorToken())
}

func (set *UserTokenSet) Save() {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	set.saveordie()
}

func (set *UserTokenSet) Touch(name string) error {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	ut := set.findByName(name)
	if ut == nil {
		return fmt.Errorf("no token by the name %s", name)
	}
	ut.LastUse = time.Now()
	set.tokens[name] = *ut
	set.saveordie()
	return nil
}

func (set *UserTokenSet) StorePersonalInfo(name string,
	pi *PersonalInfo) error {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	ut := set.findByName(name)
	if ut == nil {
		return fmt.Errorf("no token by the name %s", name)
	}
	ut.PI = *pi
	set.tokens[name] = *ut
	set.saveordie()
	return nil
}

func (set *UserTokenSet) GetOauthToken(name string) *oauth2.Token {
	ut := set.findByName(name)
	if ut == nil {
		return nil
	}
	return &ut.OauthToken
}

func (set *UserTokenSet) UpdateOauthToken(name string,
	tok oauth2.Token) error {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	ut := set.findByName(name)
	if ut == nil {
		return fmt.Errorf("no token by the name %s", name)
	}
	if ut.OauthToken.AccessToken != tok.AccessToken {
		ut.OauthToken = tok
		set.tokens[name] = *ut
		set.saveordie()
		log.Printf("updated and saved token for %s (now %s)", name,
			ut.OauthToken.AccessToken)
		// we should be done now, but as long as there remains the danger of
		// race conditions or other bugs that cause cfg.UserTokens to get
		// overwritten, I would rather not lose anybody's refresh token.
		buf, err := json.MarshalIndent(tok, "", "  ")
		if err != nil {
			log.Printf("failed encoding json: %s", err)
		} else {
			f, err := os.OpenFile("token_failsafe.json",
				os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			defer f.Close()
			if err != nil {
				log.Printf("failed to open token_failsafe.json: %s", err)
			} else {
				_, err = f.Write(buf)
				if err != nil {
					log.Printf("failed to write token_failsafe.json: %s", err)
				}
			}
		}
	}
	return nil
}

func (set *UserTokenSet) CopyUserTokens() []UserToken {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	uts := make([]UserToken, 0, len(set.tokens))
	for _, value := range set.tokens {
		uts = append(uts, value)
	}
	return uts
}

func (set *UserTokenSet) IsNew(user string) bool {
	ut := set.findByName(user)
	if ut == nil {
		return false
	}
	return ut.LastUse.IsZero()
}

func (set *UserTokenSet) FindNameById(id string) (string, error) {
	for _, i := range set.tokens {
		if i.PI.ID == id {
			return i.Name, nil
		}
	}
	return "", fmt.Errorf("no token matching id %s", id)
}

func (set *UserTokenSet) NameIsTaken(name string) bool {
	return set.findByName(name) != nil
}
