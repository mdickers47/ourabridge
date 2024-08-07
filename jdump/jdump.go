package jdump

import (
	"encoding/json"
	"log"
	"os"
)

func ParseJsonOrDie(f string, dest any) {
	bytes, err := os.ReadFile(f)
	if err != nil {
		log.Fatalf("can't read json file %s: %v", f, err)
	}
	err = json.Unmarshal(bytes, dest)
	if err != nil {
		log.Fatalf("can't parse json file %s: %v", f, err)
	}
}

func DumpJsonOrDie(f string, obj any) {
	buf, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		log.Fatalf("error encoding json: %v", err)
	}
	err = os.WriteFile(f, buf, 0600)
	if err != nil {
		log.Fatalf("error saving json file %s: %v", f, err)
	}
}
