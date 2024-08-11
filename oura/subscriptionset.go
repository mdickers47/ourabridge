package oura

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// in only these webhook structures, timestamps are given in a nonstandard
// format that encoding/json cannot parse by default.

type weirdTime time.Time

type subRequest struct {
	Callback_url       string `json:"callback_url"`
	Event_type         string `json:"event_type"`
	Data_type          string `json:"data_type"`
	Verification_token string `json:"verification_token"`
}

// the response object is just different enough from the request
// object that trying to make them into one struct causes problems.

type subResponse struct {
	ID                 string    `json:"id"`
	Callback_url       string    `json:"callback_url"`
	Event_type         string    `json:"event_type"`
	Data_type          string    `json:"data_type"`
	Verification_token string    `json:"verification_token,omitempty"`
	Expiration_time    weirdTime `json:"expiration_time,omitempty"`
	mark               bool      `json:"-"` // for mark and sweep gc
}

type SubscriptionSet struct {
	Subs []subResponse
	Lock sync.Mutex
}

func (t *weirdTime) UnmarshalJSON(b []byte) error {
	// first try and see if it's a standard-format time
	var ts time.Time
	err := json.Unmarshal(b, &ts)
	if err == nil {
		*t = weirdTime(ts)
		return nil
	}
	// if not, try the format oura gives out sometimes
	s := strings.Trim(string(b), "\"")
	ts, err = time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return err
	}
	*t = weirdTime(ts)
	return nil
}

func (t weirdTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t))
}

func (s *SubscriptionSet) Find(data_type string,
	event_type string) (int, *subResponse) {
	for i, sub := range s.Subs {
		if sub.Data_type == data_type && sub.Event_type == event_type {
			return i, &sub
		}
	}
	return -1, nil
}

func (s *SubscriptionSet) FindByID(id string) (int, *subResponse) {
	for i, sub := range s.Subs {
		if sub.ID == id {
			return i, &sub
		}
	}
	return -1, nil
}

func (s *SubscriptionSet) Replace(sub subResponse) {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	i, _ := s.Find(sub.Data_type, sub.Event_type)
	if i >= 0 {
		s.Subs = append(s.Subs[:i], s.Subs[i+1:]...)
	}
	s.Subs = append(s.Subs, sub)
}

func (s *SubscriptionSet) Delete(sub subResponse) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	i, _ := s.FindByID(sub.ID)
	if i < 0 {
		return fmt.Errorf("subscription id=%s not present in set", sub.ID)
	}
	s.Subs = append(s.Subs[:i], s.Subs[i+1:]...)
	return nil
}

func MakeSubscriptionSet() SubscriptionSet {
	s := SubscriptionSet{}
	s.Subs = make([]subResponse, 0, 8)
	return s
}
