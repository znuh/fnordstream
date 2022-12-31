package main

import (
	"log"
	"io/ioutil"
	"encoding/json"
)

func load_json(fname string, dst *map[string]interface{}) {
	content, err := ioutil.ReadFile(fname)
    if err != nil {
        log.Println("Cannot read JSON file ", fname,err)
        return
    }
    err = json.Unmarshal(content, dst)
    if err != nil {
        log.Fatal("Error during Unmarshal(): ", err)
    }
}

func save_json(fname string, src map[string]interface{}) {
	json, err := json.MarshalIndent(src, "", " ")
	if err != nil {
		log.Fatal(err)
	}
	//fmt.Println(string(json))
	err = ioutil.WriteFile(fname, json, 0644)
	if err != nil {
		log.Println(err)
	}
}
