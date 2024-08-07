package oura

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func getSubscriptions(cfg *ClientConfig, ut *UserToken) []subResponse {
	var body []byte
	var err  error
	subList := make([]subResponse, 1)
	if body, err = webhookReq(cfg, ut, "GET", "", nil); err != nil {
		log.Printf("failed to list subscriptions: %s", err)
		return subList
	}
	if err = json.Unmarshal(body, &subList); err != nil {
		log.Printf("failed to parse subscription list: %s", err)
		log.Printf("body was: %s", body)
	}
	return subList
}

func webhookReq(cfg *ClientConfig, ut *UserToken, method string, id string,
	body []byte) ([]byte, error) {
	var req *http.Request
	var res *http.Response
	var err error

	if len(ut.OauthToken.AccessToken) == 0 {
		return nil, fmt.Errorf("no access token for %s", ut.Name)
	}
	client, cancel := ut.HttpClient(cfg)
	defer cancel()

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
	log.Printf("doing %s, body is %s", method, body)
	if res, err = client.Do(req); err != nil {
		return nil, fmt.Errorf("failed to Do request: %s", err)
	}
	if body, err = validResponseBody(res); err != nil {
		return nil, err
	}
	return body, nil
}

func CreateSubscription(cfg *ClientConfig, ut *UserToken, event_type string,
	data_type string) (*subResponse, error) {

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

	// note that even while this request is hanging, oura is calling the
	// callback url with their challenge protocol.
	body, err := webhookReq(cfg, ut, "POST", "", buf)
	if err != nil {
		return nil, err
	}
	subResp := subResponse{}
	err = json.Unmarshal(body, &subResp)
	return &subResp, err
}

func RenewSubscription(cfg *ClientConfig, ut *UserToken,
	sub *subResponse) error {

	client, cancel := ut.HttpClient(cfg)
	defer cancel()
	ouraurl := cfg.OuraPath("/webhook/subscription/renew/" + sub.ID).String()

	// someone decided to be fancy with http methods, sigh
	req, err := http.NewRequest("PUT", ouraurl, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := validResponseBody(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, sub)
}

func ProcessEvent(cfg *ClientConfig, event EventNotification,
	sink chan<- Observation) {
	ut := cfg.UserTokens.FindById(event.User_id)
	if ut == nil {
		log.Printf("webhook notification for unknown user %s", event.User_id)
		return
	}
	log.Printf("received webhook notification for %s/%s/%s",
		ut.Name, event.Event_type, event.Data_type)
	if event.Event_type == "update" || event.Event_type == "create" {
		// the other possible Event_type is "delete" and there is nothing we
		// can do with that.
		switch event.Data_type {
		case "daily_activity":
			da := dailyActivity{}
			err := GetDocByID(cfg, ut, "daily_activity", event.Object_id, &da)
			if err != nil {
				SendDoc(da, ut.Name, sink)
			}
		case "daily_readiness":
			dr := dailyReadiness{}
			err := GetDocByID(cfg, ut, "daily_readiness", event.Object_id, &dr)
			if err != nil {
				SendDoc(dr, ut.Name, sink)
			}
		case "daily_sleep":
			ds := dailySleep{}
			err := GetDocByID(cfg, ut, "daily_sleep", event.Object_id, &ds)
			if err != nil {
				SendDoc(ds, ut.Name, sink)
			}
		case "sleep":
			sp := sleepPeriod{}
			err := GetDocByID(cfg, ut, "sleep", event.Object_id, &sp)
			if err != nil {
				SendDoc(sp, ut.Name, sink)
			}
		default:
			// unhandled types include:
			// tag enhanced_tag workout session daily_spo2 sleep_time
			// rest_mode_period ring_configuration daily_stress
			// daily_cycle_phases
			log.Printf("webhook notification for unhandled type: %s",
				event.Data_type)
		}
	}
}

func ValidateSubscriptions(cfg *ClientConfig) {
	// ask oura what subscriptions it thinks we have, and check to see if
	// we have them in our list.
	for _, ut := range cfg.UserTokens.Tokens {
		for _, sr := range getSubscriptions(cfg, &ut) {
			found := false
			for _, sub := range ut.Subscriptions {
				if sub.ID == sr.ID {
					// we actually did know about this subscription, lucky day
					ut.ReplaceSubscription(sr)
					found = true
				}
			}
			if !found {
				// we don't know what the parameters of this subscription are,
				// because we lost them and they are not included in "List
				// Webhook Subscriptions."  all we can do is delete it.
				log.Printf("deleting subscription %s", sr.ID)
				if _, err := webhookReq(cfg, &ut, "DELETE", sr.ID, nil); err != nil {
					log.Printf("failed to delete mystery subscription: %s", err)
				}
			}				
		}
	}
}
