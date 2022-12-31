package main

import (
	"fmt"
	"os"
	"bufio"
	"strings"
	"regexp"
)

func load_file(specname string, streams *[]interface{}, viewports *[]interface{}, options map[string]bool) {
	fh := os.Stdin
	if specname != "-" {
		fh, _ = os.Open(specname)
	}
	reader    := bufio.NewReader(fh)
	re := regexp.MustCompile(`(\S+)\s*=\s*(\S+)`)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		var uri string
		var geo Geometry
		res, _ := fmt.Sscanf(line,"%s %d %d %d %d",&uri,&geo.W,&geo.H,&geo.X,&geo.Y)
		if res >= 1 {
			if strings.Contains(line,"=") && !strings.Contains(line,"http") { // ugly hack for now
				res := re.FindStringSubmatch(line)
				if len(res) < 3 { continue }
				options[res[1]] = res[2] == "true"
			} else {
				*streams = append(*streams, uri)
				fmt.Print("added "+line)
				if res == 5 {
					*viewports = append(*viewports, geo)
				}
			}
		}
	}
	fh.Close()
}

func console_client(shub *StreamHub, specname string, wait bool) {
	fmt.Println("adding streams via console client")

	profiles := map[string]interface{}{};
	load_json("stream_profiles.json", &profiles);

	streams   := []interface{}{};
	viewports := []interface{}{};
	options   := map[string]bool{  // add some sane? defaults
		"start_muted"   : true,
		"restart_error" : true,
	};

	/* try loading spec from JSON profiles first */
	profile, ok := profiles[specname].(map[string]interface{})
	if ok {
		streams, _   = profile["stream_locations"].([]interface{})
		if profile["viewports"] != nil {
			viewports, _ = profile["viewports"].([]interface{})
		}
		if profile["options"] != nil {
			options, _   = profile["options"].(map[string]bool)
		}
	}

	if len(streams)<1 {
		load_file(specname, &streams, &viewports, options)
	}

	if len(streams) < 1 {
		fmt.Println("no streams given - nothing to do")
		return
	}

	client := &Client{
		client_request : make(chan map[string]interface{}),
	}
	if wait {
		client.client_notify = make(chan []byte)
	}
	shub.Register <- client

	msg := map[string]interface{}{
		"request"   : "start_streams",
		"streams"   : streams,
		"viewports" : viewports,
		"options"   : options,
	}

	client.client_request <- msg

	if wait {
		for {
			<- client.client_notify
		}
	}

	shub.Unregister <- client
	close(client.client_request)
}
