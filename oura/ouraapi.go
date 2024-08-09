package oura

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

func isSuccess(code int) bool {
	// this is hacky but what else is there to do here?  typing out a
	// long list of all the possible success codes is not less hacky.
	return code >= 200 && code < 300
}

func validUrl(flagval *string) *url.URL {
	v_url, err := url.Parse(*flagval)
	if err != nil {
		log.Fatalf("bogus url value: %s %v", flagval, err)
	}
	return v_url
}

func validResponseBody(res *http.Response) ([]byte, error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return []byte{}, err
	} else if !isSuccess(res.StatusCode) {
		msg := fmt.Sprintf("oura http status was %s", res.Status)
		log.Printf(msg)
		log.Printf("oura response body: %s", body)
		return []byte{}, fmt.Errorf(msg)
	}
	return body, nil
}

func process[T Doc](err error, doclist []T, name string,
	sink chan<- Observation) int {
	if err != nil {
		log.Printf("document search failed: %s", err)
	}
	sent_count := 0
	for _, doc := range doclist {
		// I tried to use document timestamps to avoid saving duplicate
		// observations, but it doesn't work without getting complicated,
		// because documents arrive with back-dated timestamps.
		sent_count += SendDoc(doc, name, sink)
	}
	log.Printf("retrieved %d documents for %d observations",
		len(doclist), sent_count)
	return sent_count
}

func doGet(cfg *ClientConfig, user string, ouraurl string, pDest any) error {

	client, cancel := cfg.OauthClient(user)
	defer cancel()
	log.Printf("doing GET %s", ouraurl)
	res, err := client.Get(ouraurl)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if !isSuccess(res.StatusCode) {
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

func GetDocByID(cfg *ClientConfig, user string, endpoint string,
	id string, pDest any) error {
	ouraurl := cfg.OuraPath("/usercollection/" + endpoint + "/" + id)
	return doGet(cfg, user, ouraurl.String(), pDest)
}

func SearchAll(cfg *ClientConfig, name string, sink chan<- Observation) {
	dr := readinessResponse{}
	err := SearchDocs(cfg, name, "daily_readiness", &dr)
	process(err, dr.Data, name, sink)
	da := activityResponse{}
	err = SearchDocs(cfg, name, "daily_activity", &da)
	process(err, da.Data, name, sink)
	ds := sleepResponse{}
	err = SearchDocs(cfg, name, "daily_sleep", &ds)
	process(err, ds.Data, name, sink)
	dp := sleepPeriodResponse{}
	err = SearchDocs(cfg, name, "sleep", &dp)
	process(err, dp.Data, name, sink)
	hr := heartrateResponse{}
	err = SearchDocs(cfg, name, "heartrate", &hr)
	process(err, hr.Data, name, sink)
	cfg.UserTokens.Touch(name)
}

func SearchDocs(cfg *ClientConfig, name string, endpoint string,
	pDest any) error {
	ts := func(d time.Duration) string {
		// go time.Format is odd
		return time.Now().Add(d).Format("2006-01-02")
	}
	backfill_days := 1
	if cfg.UserTokens.IsNew(name) {
		// if we have never seen you before, start by searching backwards
		// 7 days
		backfill_days = 7
	}
	params := url.Values{}
	params.Add("start_date", ts(-24*time.Hour*time.Duration(backfill_days)))
	params.Add("end_date", ts(+24*time.Hour))
	ouraurl := cfg.OuraPath("/usercollection/" + endpoint)
	ouraurl.RawQuery = params.Encode()
	return doGet(cfg, name, ouraurl.String(), pDest)
}

func RandomString() string {
	nonce := make([]byte, 18)
	rand.Read(nonce)
	return base64.URLEncoding.EncodeToString(nonce)
}

/* oh god we have a function that uses generics and reflection */

func SendDoc[T Doc](doc T, username string, sink chan<- Observation) int {
	sent_count := 0
	send_ts := func(k string, v float32, t time.Time) {
		sink <- Observation{
			Timestamp: t,
			Username:  username,
			Field:     fmt.Sprintf("%s.%s", doc.GetMetricPrefix(), k),
			Value:     v,
		}
		sent_count += 1
	}
	send := func(k string, v float32) {
		send_ts(k, v, doc.GetTimestamp())
	}
	s := reflect.ValueOf(&doc).Elem()
	typeOfDoc := s.Type()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		metric_name := strings.ToLower(typeOfDoc.Field(i).Name)
		// some types that we can always handle the same way
		switch f.Type().Name() {
		case "int":
			send(metric_name, float32(f.Interface().(int)))
		case "float32":
			send(metric_name, f.Interface().(float32))
		case "intervalMetric":
			im := f.Interface().(intervalMetric)
			for i, v := range im.Items {
				t := im.Timestamp.Add(
					time.Duration(float32(i)*im.Interval) * time.Second)
				// these timeseries contain 'null' which go parses as 0; luckily it
				// will never be a valid heart_rate or hrv or met
				if v > 0.0 {
					send_ts(metric_name, v, t)
				}
			}
		}
		// special oddball cases
		switch metric_name {
		case "contributors":
			for k, v := range f.Interface().(map[string]int) {
				send(fmt.Sprintf("contrib.%s", strings.ToLower(k)), float32(v))
			}
		case "movement_30_sec":
			s := f.Interface().(string)
			// GetTimestamp() returns bedtime_end on sleepPeriod, but we can find
			// bedtime_start anyway
			t0 := doc.GetTimestamp().Add(time.Duration(-30*len(s)) * time.Second)
			for i := 0; i < len(s); i++ {
				send_ts(metric_name, float32(s[i]-48),
					t0.Add(time.Duration(30*i)*time.Second))
			}
		case "sleep_phase_5_min":
			s := f.Interface().(string)
			t0 := doc.GetTimestamp().Add(time.Duration(-300*len(s)) * time.Second)
			for i := 0; i < len(s); i++ {
				send_ts(metric_name, float32(s[i]-48),
					t0.Add(time.Duration(300*i)*time.Second))
			}
		}

	}
	return sent_count
}
