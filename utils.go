package main

import (
	"log"
	"strings"
	"io/ioutil"
	"encoding/json"

	"github.com/go-cmd/cmd"
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

type CmdInfo struct {
	ExitCode   int     `json:"exit_code" mapstructure:"exit_code"`
	Stdout     string  `json:"stdout,omitempty" mapstructure:"stdout"`
	Error      string  `json:"error,omitempty" mapstructure:"error"`
}

func probe_command(command string) *CmdInfo {
	ctx    := cmd.NewCmd(command, "--version")
	status := <-ctx.Start()

	res := &CmdInfo{
		ExitCode : status.Exit,
	}
	if status.Error != nil {
		res.Error = status.Error.Error()
	}
	if len(status.Stdout) > 0 {
		res.Stdout = strings.Join(status.Stdout, "\n")
	}
    return res
}
