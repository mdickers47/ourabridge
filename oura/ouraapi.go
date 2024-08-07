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

func process[T Doc](err error, doclist []T, ut *UserToken,
	sink chan<- Observation) int {
	if err != nil {
		log.Printf("document search failed: %s", err)
	}
	sent_count := 0
	for _, doc := range doclist {
		// I tried to use document timestamps to avoid saving duplicate
		// observations, but it doesn't work without getting complicated,
		// because documents arrive with back-dated timestamps.
		sent_count += SendDoc(doc, ut.Name, sink)
	}
	log.Printf("retrieved %d documents for %d observations",
		len(doclist), sent_count)
	return sent_count
}

func GetDocByID(cfg *ClientConfig, ut *UserToken, endpoint string,
	id string, pDest any) error {
	ouraurl := cfg.OuraPath("/usercollection/" + endpoint + "/" + id)
	return doGet(cfg, ut, ouraurl.String(), pDest)
}

func SearchAll(cfg *ClientConfig, ut *UserToken, sink chan<- Observation) {
	dr := readinessResponse{}
	err := SearchDocs(cfg, ut, "daily_readiness", &dr)
	process(err, dr.Data, ut, sink)
	da := activityResponse{}
	err = SearchDocs(cfg, ut, "daily_activity", &da)
	process(err, da.Data, ut, sink)
	ds := sleepResponse{}
	err = SearchDocs(cfg, ut, "daily_sleep", &ds)
	process(err, ds.Data, ut, sink)
	dp := sleepPeriodResponse{}
	err = SearchDocs(cfg, ut, "sleep", &dp)
	process(err, dp.Data, ut, sink)
	hr := heartrateResponse{}
	err = SearchDocs(cfg, ut, "heartrate", &hr)
	process(err, hr.Data, ut, sink)
	ut.LastPoll = time.Now()
	cfg.UserTokens.Replace(ut.Name, *ut)
}

func SearchDocs(cfg *ClientConfig, ut *UserToken, endpoint string,
	pDest any) error {
	ts := func(d time.Duration) string {
		// go time.Format is odd
		return time.Now().Add(d).Format("2006-01-02")
	}
	backfill_days := 1
	if ut.LastPoll.IsZero() {
		// if we have never seen you before, start by searching backwards
		// 7 days
		backfill_days = 7
	}
	params := url.Values{}
	params.Add("start_date", ts(-24*time.Hour*time.Duration(backfill_days)))
	params.Add("end_date", ts(+24*time.Hour))
	ouraurl := cfg.OuraPath("/usercollection/" + endpoint)
	ouraurl.RawQuery = params.Encode()
	return doGet(cfg, ut, ouraurl.String(), pDest)
}

func doGet(cfg *ClientConfig, ut *UserToken, ouraurl string, pDest any) error {

	if len(ut.OauthToken.AccessToken) == 0 {
		return fmt.Errorf("no access token for %s", ut.Name)
	}

	client, cancel := ut.HttpClient(cfg)
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

func RandomString() string {
	nonce := make([]byte, 18)
	rand.Read(nonce)
	return base64.URLEncoding.EncodeToString(nonce)
}

/* oh god we have a function that uses generics and reflection */

func SendDoc[T Doc](doc T, username string, sink chan<- Observation) int {
	sent_count := 0
	send := func(k string, v float32) {
		sink <- Observation{
			Timestamp: doc.GetTimestamp(),
			Username:  username,
			Field:     fmt.Sprintf("%s.%s", doc.GetMetricPrefix(), k),
			Value:     v,
		}
		sent_count += 1
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
	return sent_count
}
