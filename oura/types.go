package oura

import (
	"fmt"
	"strings"
	"time"
)

type Doc interface {
	dailyReadiness | dailyActivity | dailySleep | sleepPeriod | heartrateInstant | dailySpo2 | dailyResilience | dailyStress
	GetTimestamp() time.Time
	GetMetricPrefix() string
}

type PersonalInfo struct {
	ID             string
	Age            int
	Weight         float32
	Height         float32
	Biological_sex string
	Email          string
}

// a substructure that appears when they want to give you something in
// a 1-minly array but only want to include it in a daily document for
// some reason
type intervalMetric struct {
	Interval  float32
	Items     []float32
	Timestamp time.Time
}

type dailyReadiness struct { // implements Doc
	ID                          string
	Contributors                map[string]int
	Score                       int
	Temperature_deviation       float32
	Temperature_trend_deviation float32
	Day                         string
	Timestamp                   time.Time
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
	Met                         intervalMetric
	Meters_to_target            int
	Non_wear_time               int
	Resting_time                int
	Sedentary_met_minutes       int
	Sedentary_time              int
	Steps                       int
	Target_calories             int
	Target_meters               int
	Total_calories              int
	Day                         string
	Timestamp                   time.Time
}

// this comes as a string, we want an int, so we need custom Marshal
// and Unmarshal handlers
type resilienceLevel int

type dailyResilience struct {
	ID           string
	Day          string
	Contributors map[string]float32 // watch out, this is unlike the others
	Level        resilienceLevel
}

type dailySleep struct {
	ID           string
	Contributors map[string]int
	Day          string
	Score        int
	Timestamp    time.Time
}

type sleepPeriod struct {
	ID                   string
	Average_breath       float32
	Average_heart_rate   float32
	Average_hrv          int
	Awake_time           int
	Bedtime_end          time.Time
	Bedtime_start        time.Time
	Day                  string
	Deep_sleep_duration  int
	Efficiency           int
	Heart_rate           intervalMetric
	Hrv                  intervalMetric
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

type heartrateInstant struct {
	Bpm       int
	Source    string
	Timestamp time.Time
}

// This one contains a pointless nested data structure that forces us
// to treat it as a special case everywhere.
type dailySpo2 struct {
	ID              string
	Day             string
	Spo2_percentage struct {
		Average float32
	}
}

type dailyStress struct {
	ID            string
	Day           string
	Stress_high   int
	Recovery_high int
	Day_summary   string
}

type SearchResponse[D Doc] struct {
	Data       []D
	Next_token string
}

// the "webhook subscription" will send you these in POST requests.
type EventNotification struct {
	Event_type string
	Data_type  string
	Object_id  string
	Event_time time.Time
	User_id    string
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

func (ds dailySpo2) GetTimestamp() time.Time {
	// this document lacks Timestamp and has only Day.  So you're
	// getting the time.Time zero value if the parsing fails, sorry.
	t, _ := time.Parse("2006-01-02", ds.Day)
	return t
}

func (ds dailySpo2) GetMetricPrefix() string {
	return "spo2"
}

func (dr dailyResilience) GetTimestamp() time.Time {
	t, _ := time.Parse("2006-01-02", dr.Day)
	return t
}

func (dr dailyResilience) GetMetricPrefix() string {
	return "resilience"
}

func (ds dailyStress) GetTimestamp() time.Time {
	t, _ := time.Parse("2006-01-02", ds.Day)
	return t
}

func (ds dailyStress) GetMetricPrefix() string {
	return "stress"
}

func (r *resilienceLevel) UnmarshalJSON(b []byte) error {
	levels := map[string]resilienceLevel{
		"limited":     1,
		"adequate":    2,
		"solid":       3, // I am assuming the words above "adequate" look like this
		"strong":      4, // because I have never seen them lol
		"exceptional": 5,
	}
	s := strings.Trim(string(b), "\"")
	i, ok := levels[s]
	if !ok {
		*r = -1
		return fmt.Errorf("unknown resilience level %s", s)
	}
	*r = i
	return nil
}

func (r resilienceLevel) MarshalJSON() ([]byte, error) {
	levels := map[resilienceLevel]string{
		1: "limited",
		2: "adequate",
		3: "solid",
		4: "strong",
		5: "exceptional",
	}
	s, ok := levels[r]
	if !ok {
		return nil, fmt.Errorf("unknown resilience level %d", r)
	}
	return []byte(s), nil
}
