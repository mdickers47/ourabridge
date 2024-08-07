package oura

import (
	"log"
	"sync"

	"github.com/mdickers47/ourabridge/jdump"
)

// a userTokenSet is a container for a bunch of UserTokens that you
// should only access through its methods.  It tries to preserve
// itself in json form in the given File.
type UserTokenSet struct {
	Tokens map[string]UserToken
	Lock   sync.Mutex
	File   string
}

func (set *UserTokenSet) FindById(id string) *UserToken {
	for _, i := range set.Tokens {
		if i.PI.ID == id {
			return &i
		}
	}
	return nil
}

func (set *UserTokenSet) FindByName(name string) *UserToken {
	// probably overkill but maybe this implementation changes
	// some day
	ut, ok := set.Tokens[name]
	if !ok {
		return nil
	} else {
		return &ut
	}
}

func (set *UserTokenSet) Replace(name string, ut UserToken) {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	set.Tokens[name] = ut
	jdump.DumpJsonOrDie(set.File, set.Tokens)
	log.Printf("updated and saved token for %s: %s", name, ut.CensorToken())
}

func (set *UserTokenSet) Save() {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	jdump.DumpJsonOrDie(set.File, set.Tokens)
}

func (set *UserTokenSet) CopyUserTokens() []UserToken {
	set.Lock.Lock()
	defer set.Lock.Unlock()
	uts := make([]UserToken, 0, len(set.Tokens))
	for _, value := range set.Tokens {
		uts = append(uts, value)
	}
	return uts
}
