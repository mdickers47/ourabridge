package oura

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// unlike the rest of the API, the endpoint /webhook/subscription is
// unattached to any oauth token.  It uses only your ClientID and
// ClientSecret, which go in custom headers.  This is your clue that,
// unlike the rest of the API, subscriptions are only defined by
// (client, data_type, event_type), and are not associated with any
// userID.  Oura remembers somewhere what userIDs your client is
// interested in, and sends notifications for all of them.  You then
// have to have an oauth token for that userID to retrieve the
// document.  None of this is written in the documentation.

func webhookReq(cfg *ClientConfig, method string, id string,
	body []byte) ([]byte, error) {
	var req *http.Request
	var res *http.Response
	var err error

	// the "create subscription" operation in particular is very slow
	client := http.Client{Timeout: 75 * time.Second}
	path := "/webhook/subscription"
	if len(id) > 0 {
		path += "/" + id
	}
	dest := cfg.OuraPath(path).String()
	req, err = http.NewRequest(method, dest, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed in NewRequest: %s", err)
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("x-client-id", cfg.OauthConfig.ClientID)
	req.Header.Set("x-client-secret", cfg.OauthConfig.ClientSecret)
	log.Printf("doing %s %s, body is %s", method, dest, body)
	if res, err = client.Do(req); err != nil {
		return nil, fmt.Errorf("failed to Do request: %s", err)
	}
	if body, err = validResponseBody(res); err != nil {
		return nil, err
	}
	return body, nil
}

func getSubscriptions(cfg *ClientConfig) []subResponse {
	var body []byte
	var err error
	subList := make([]subResponse, 0, 8)
	if body, err = webhookReq(cfg, "GET", "", nil); err != nil {
		log.Printf("failed to list subscriptions: %s", err)
		return subList
	}
	if err = json.Unmarshal(body, &subList); err != nil {
		log.Printf("failed to parse subscription list: %s", err)
		log.Printf("body was: %s", body)
	}
	return subList
}

func CreateSubscription(cfg *ClientConfig, data_type string,
	event_type string) (*subResponse, error) {

	// this goes in basically a global variable because a http.Handler
	// is going to need to verify it.  so uh don't like call this
	// function a lot of times at once.  if you need to do that, the
	// Verifier will need to become a map[username]string or something
	// fancy.
	cfg.Verifier = RandomString()
	sub := subRequest{
		Callback_url:       cfg.MyPath("/event").String(),
		Verification_token: cfg.Verifier,
		Event_type:         event_type,
		Data_type:          data_type,
	}

	buf, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}

	// note that while this request is hanging, oura is calling the
	// callback url with their challenge protocol.
	body, err := webhookReq(cfg, "POST", "", buf)
	if err != nil {
		return nil, err
	}
	subResp := subResponse{}
	err = json.Unmarshal(body, &subResp)
	return &subResp, err
}

func RenewSubscription(cfg *ClientConfig, sub *subResponse) error {
	body, err := webhookReq(cfg, "PUT", "renew/"+sub.ID, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, sub)
}

func ProcessEvent(cfg *ClientConfig, event EventNotification,
	sink chan<- Observation) {

	user, err := cfg.UserTokens.FindNameById(event.User_id)
	if err != nil {
		log.Printf("webhook notification for unknown userid %s", event.User_id)
		return
	}

	log.Printf("received webhook notification for %s/%s/%s",
		user, event.Event_type, event.Data_type)
	if event.Event_type == "update" || event.Event_type == "create" {
		// the other possible Event_type is "delete" and there is nothing we
		// can do with that.
		var err error
		var i int
		switch event.Data_type {
		case "daily_activity":
			da := dailyActivity{}
			err = GetDocByID(cfg, user, "daily_activity", event.Object_id, &da)
			if err == nil {
				i = SendDoc(da, user, sink)
			}
		case "daily_readiness":
			dr := dailyReadiness{}
			err = GetDocByID(cfg, user, "daily_readiness", event.Object_id, &dr)
			if err == nil {
				i = SendDoc(dr, user, sink)
			}
		case "daily_sleep":
			ds := dailySleep{}
			err = GetDocByID(cfg, user, "daily_sleep", event.Object_id, &ds)
			if err == nil {
				i = SendDoc(ds, user, sink)
			}
		case "sleep":
			sp := sleepPeriod{}
			err = GetDocByID(cfg, user, "sleep", event.Object_id, &sp)
			if err == nil {
				i = SendDoc(sp, user, sink)
			}
		default:
			// unhandled types include:
			// tag enhanced_tag workout session daily_spo2 sleep_time
			// rest_mode_period ring_configuration daily_stress
			// daily_cycle_phases
			err = fmt.Errorf("unhandled notification type: %s", event.Data_type)
		}
		if err != nil {
			log.Printf("failed to retrieve document %s: %s", event.Object_id, err)
		} else {
			log.Printf("%s document id=%s processed for %d observations",
				event.Data_type, event.Object_id, i)
			cfg.UserTokens.Touch(user)
		}
	}
}

func ValidateSubscriptions(cfg *ClientConfig) {
	// clear garbage collection flags
	for _, sub := range cfg.Subscriptions.Subs {
		sub.mark = false
	}
	// ask oura what subscriptions it thinks we have, and check to see if
	// we have them in our list.
	for _, sr := range getSubscriptions(cfg) {
		sr.mark = true
		cfg.Subscriptions.Replace(sr)
		/*
			found := false
			for _, sub := range cfg.Subscriptions.Subs {
				if sub.ID == sr.ID {
					// we actually did know about this subscription, lucky day.
					// we save the new copy oura gave us, in case Expiration_time
					// has changed or something.
					sr.mark = true
					cfg.Subscriptions.Replace(sr)
					found = true
				}
			}
			if !found {
				// we don't know what the parameters of this subscription are,
				// because we lost them and they are not included in "List
				// Webhook Subscriptions."  all we can do is delete it.
				log.Printf("deleting subscription %s", sr.ID)
				if _, err := webhookReq(cfg, "DELETE", sr.ID, nil); err != nil {
					log.Printf("failed to delete mystery subscription: %s", err)
				}
			}
		*/
	}

	// delete our memory of any subscription that oura does not know about
	for _, sub := range cfg.Subscriptions.Subs {
		if !sub.mark {
			cfg.Subscriptions.Delete(sub)
			log.Printf("deleted forgotten subscription %s/%s id=%s",
				sub.Data_type, sub.Event_type, sub.ID)
		}
	}

	// now that the local list is in agreement with oura, check that we
	// have a subscription for every document type we like, renew any
	// that are short-dated, and [try to] create the ones that are
	// missing
	api_fail_count := 0
	checkFail := func(err error) bool {
		if err != nil {
			log.Printf("webhook api call failed: %s", err)
			if api_fail_count += 1; api_fail_count > 3 {
				log.Printf("too many webhook api failures, giving up for now")
				return true
			}
		}
		return false
	}

	for _, data_type := range []string{"daily_activity", "daily_readiness",
		"daily_sleep", "sleep"} {
		for _, event_type := range []string{"create", "update"} {
			_, sub := cfg.Subscriptions.Find(data_type, event_type)
			if sub == nil {
				// no subscription of this data_type/event_type exists
				sub, err := CreateSubscription(cfg, data_type, event_type)
				if err == nil {
					cfg.Subscriptions.Replace(*sub)
				} else {
					if checkFail(err) {
						return
					}
				}
			} else {
				// we think we have this subscription already
				lifetime := time.Time(sub.Expiration_time).Sub(time.Now())
				// the expiration dates are observed to be weeks in the future
				if lifetime < 24*time.Hour {
					if err := RenewSubscription(cfg, sub); err != nil {
						if checkFail(err) {
							return
						}
					} else {
						log.Printf("renewed subscription %s/%s until %s",
							sub.Data_type, sub.Event_type, time.Time(sub.Expiration_time))
					}
				} else {
					// we have a subscription and it is unexpired
					log.Printf("subscription %s/%s is good until %s",
						sub.Data_type, sub.Event_type, time.Time(sub.Expiration_time))
				}
			} // if sub == nil
		} // for event_type
	} // for data_type
}
