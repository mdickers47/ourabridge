package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"io"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

type hasTimestamp interface {
	GetTimestamp() time.Time
}

type ouraDoc interface {
	dailyReadiness | dailyActivity | dailySleep | sleepPeriod | heartrateInstant
	GetTimestamp() time.Time
	GetMetricPrefix() string
}

type personalInfo struct {
	ID             string
	Age            int
	Weight         float32
	Height         float32
	Biological_sex string
	Email          string
}

type readinessResponse struct {
	Data       []dailyReadiness
	Next_token string
}

type dailyReadiness struct {
	ID                          string
	Contributors                map[string]int
	Score                       int
	Temperature_deviation       float32
	Temperature_trend_deviation float32
	Day                         string
	Timestamp                   time.Time
}

type activityResponse struct {
	Data       []dailyActivity
	Next_token string
}

type dailyActivity struct {
	ID                          string
	Class_5_min                 string
	Score                       int
	Active_calories             int
	Average_met_minutes         float32
	Contributors                map[string]int
	Equivalent_walking_distance int
	High_activity_met_minutes   int
	High_activity_time          int
	Inactivity_alerts           int
	Low_activity_met_minutes    int
	Low_activity_time           int
	Medium_activity_met_minutes int
	Medium_activity_time        int
	Met                         struct {
		Interval  float32
		Items     []float32
		Timestamp time.Time
	}
	Meters_to_target      int
	Non_wear_time         int
	Resting_time          int
	Sedentary_met_minutes int
	Sedentary_time        int
	Steps                 int
	Target_calories       int
	Target_meters         int
	Total_calories        int
	Day                   string
	Timestamp             time.Time
}

type resilienceResponse struct {
	Data       []dailyResilience
	Next_token string
}

type dailyResilience struct {
	ID           string
	Day          string
	Contributors map[string]int
	Level        string
}

type sleepResponse struct {
	Data       []dailySleep
	Next_token string
}

type dailySleep struct {
	ID           string
	Contributors map[string]int
	Day          string
	Score        int
	Timestamp    time.Time
}

type sleepPeriodResponse struct {
	Data       []sleepPeriod
	Next_token string
}

type sleepPeriod struct {
	ID                  string
	Average_breath      float32
	Average_heart_rate  float32
	Average_hrv         int
	Awake_time          int
	Bedtime_end         time.Time
	Bedtime_start       time.Time
	Day                 string
	Deep_sleep_duration int
	Efficiency          int
	Heart_rate          struct {
		Interval  float32
		Items     []float32
		Timestamp time.Time
	}
	Hrv struct {
		Interval  float32
		Items     []float32
		Timestamp time.Time
	}
	Latency              int
	Light_sleep_duration int
	Low_battery_alert    bool
	Lowest_heart_rate    int
	Movement_30_sec      string
	Period               int
	Readiness            struct {
		Contributors                map[string]int
		Score                       int
		Temperature_deviation       float32
		Temperature_trend_deviation float32
	}
	Readiness_score_delta   int
	Rem_sleep_duration      int
	Restless_periods        int
	Sleep_phase_5_min       string
	Sleep_score_delta       int
	Sleep_algorithm_version string
	Time_in_bed             int
	Total_sleep_duration    int
	Type                    string
}

type heartrateResponse struct {
	Data       []heartrateInstant
	Next_token string
}

type heartrateInstant struct {
	Bpm       int
	Source    string
	Timestamp time.Time
}

// the "webhook subscription" will send you these in POST requests.

type eventNotification struct {
	Event_type string
	Data_type  string
	Object_id  string
	Event_time time.Time
	User_id    string
}

type subRequest struct {
	Callback_url       string `json:"callback_url"`
	Event_type         string `json:"event_type"`
	Data_type          string `json:"data_type"`
	Verification_token string `json:"verification_token,omitempty"`
}

type subResponse struct {
	subRequest                // not sure this works lol
	ID              string    `json:"id,omitempty"`
	Expiration_time time.Time `json:"expiration_time,omitempty"`
}

var OuraApi = flag.String("ouraapi", "https://api.ouraring.com/v2",
	"Base URL to Oura API entry points")
var SubscriptionToken string

func validUrl(flagval *string) *url.URL {
	v_url, err := url.Parse(*flagval)
	if err != nil {
		log.Fatalf("bogus url value: %s %v", flagval, err)
	}
	return v_url
}

func validResponseBody(res *http.Response) []byte {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("failed to read response body: %v", err)
		return []byte{}
	} else if res.StatusCode != http.StatusOK {
		log.Printf("oura http status was %s", res.Status)
		log.Printf("oura response body: %s", body)
		return []byte{}
	}
	return body
}

func getOuraDoc(pUt *userToken, endpoint string,
	id string, pDest any) error {
	err := getOuraDocById(pUt, endpoint, id, pDest)
	if err != nil {
		log.Printf("error fetching document: %v", err)
	}
	return err
}

func getOuraDocById(pUt *userToken, endpoint string, id string, pDest any) error {
	ouraurl := validUrl(OuraApi)
	ouraurl.Path += "/usercollection/" + endpoint + id
	return doOuraGet(pUt, ouraurl, pDest)
}

func doOuraDocRequest(pUt *userToken, endpoint string, pDest any) error {
	ts := func(d time.Duration) string {
		// go time.Format is odd
		return time.Now().Add(d).Format("2006-01-02")
	}
	backfill_days := 1
	if pUt.LastPoll.IsZero() {
		// if we have never seen you before, start by searching backwards
		// 7 days
		backfill_days = 7
	}
	params := url.Values{}
	params.Add("start_date", ts(-24*time.Hour*time.Duration(backfill_days)))
	params.Add("end_date", ts(+24*time.Hour))

	ouraurl := validUrl(OuraApi)
	ouraurl.Path += "/usercollection/" + endpoint
	ouraurl.RawQuery = params.Encode()

	return doOuraGet(pUt, ouraurl, pDest)
}

func doOuraGet(pUt *userToken, ouraurl *url.URL,
	pDest any) error {

	client, cancel := pUt.HttpClient()
	defer cancel()
	log.Printf("doing GET %s", ouraurl.String())
	res, err := client.Get(ouraurl.String())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		log.Printf("error %d response was %s", res.StatusCode, body)
		return fmt.Errorf("http response code was %v", res.StatusCode)
	}
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(buf, pDest)
	if err != nil {
		log.Printf("unparseable response is: %s", buf)
		return err
	}

	return nil
}

/* oh god we have a function that uses generics and reflection */

func SendDoc[T ouraDoc](doc T, username string) {

	send := func(k string, v float32) {
		observationChan <- observation{
			Timestamp: doc.GetTimestamp(),
			Username:  username,
			Field:     fmt.Sprintf("%s.%s", doc.GetMetricPrefix(), k),
			Value:     v,
		}
	}

	s := reflect.ValueOf(&doc).Elem()
	typeOfDoc := s.Type()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		metric_name := strings.ToLower(typeOfDoc.Field(i).Name)
		switch f.Type().Name() {
		case "int":
			send(metric_name, float32(f.Interface().(int)))
		case "float32":
			send(metric_name, f.Interface().(float32))
		}
		if metric_name == "contributors" {
			for k, v := range f.Interface().(map[string]int) {
				send(fmt.Sprintf("contrib.%s", strings.ToLower(k)), float32(v))
			}
		}
	}
}

func createOuraSubscription(pUt *userToken, event_type string,
	data_type string) (*subResponse, error) {

	client, cancel := pUt.HttpClient()
	defer cancel()
	ouraurl := validUrl(OuraApi)
	ouraurl.Path += "/webhook/subscription"

	c_url := validUrl(BaseUrl)
	c_url.Path += "/event"

	// this goes in a global variable because a http.Handler is going to
	// need to verify it, and I don't know how else to get it there.  so
	// uh don't like call this function a lot of times at once.
	SubscriptionToken = randomString()
	sub := subRequest{
		Callback_url:       c_url.String(),
		Verification_token: SubscriptionToken,
		Event_type:         event_type,
		Data_type:          data_type,
	}

	buf, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return nil, err
	}

	log.Printf("doing POST to %s, body is: %s", ouraurl.String(), buf)

	req, err := http.NewRequest("POST", ouraurl.String(), bytes.NewBuffer(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-type", "application/json")
	req.Header.Set("x-client-id", oauthConfig.ClientID)
	req.Header.Set("x-client-secret", oauthConfig.ClientSecret)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	// note that even while this request is hanging, oura is calling the
	// callback url with their challenge protocol.

	body := validResponseBody(res)
	subResp := subResponse{}
	err = json.Unmarshal(body, &subResp)
	return &subResp, err
}

func renewOuraSubscription(pCfg *oauth2.Config, pUt *userToken,
	sub *subResponse) error {

	client, cancel := pUt.HttpClient()
	defer cancel()
	ouraurl := validUrl(OuraApi)
	ouraurl.Path += "/webhook/subscription/renew/" + sub.ID

	// someone decided to be fancy with http methods, sigh
	req, err := http.NewRequest("PUT", ouraurl.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body := validResponseBody(res)
	return json.Unmarshal(body, sub)
}

/* this is corny, but seems to be what you have to do to take
   advantage of interfaces, which are only defined as methods, not
   variables/members */

func (dr dailyReadiness) GetTimestamp() time.Time {
	return dr.Timestamp
}

func (dr dailyReadiness) GetMetricPrefix() string {
	return "readiness"
}

func (da dailyActivity) GetTimestamp() time.Time {
	return da.Timestamp
}

func (da dailyActivity) GetMetricPrefix() string {
	return "activity"
}

func (ds dailySleep) GetTimestamp() time.Time {
	return ds.Timestamp
}

func (ds dailySleep) GetMetricPrefix() string {
	return "sleep"
}

func (pi personalInfo) GetTimestamp() time.Time {
	// this is cheating; this document does not include timestamp
	return time.Now()
}

func (pi personalInfo) GetMetricPrefix() string {
	return "info"
}

func (sp sleepPeriod) GetTimestamp() time.Time {
	return sp.Bedtime_end
}

func (sp sleepPeriod) GetMetricPrefix() string {
	// danger of overwriting metrics from dailySleep document, but they
	// don't appear to contain any of the same keys??
	return "sleep"
}

func (hr heartrateInstant) GetTimestamp() time.Time {
	return hr.Timestamp
}

func (hr heartrateInstant) GetMetricPrefix() string {
	return "hr"
}
