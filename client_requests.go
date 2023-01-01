package main

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"encoding/json"
	"github.com/mitchellh/mapstructure"
)

type RequestHandler func(*StreamHub, *Client, map[string]interface {})

func set_displays(hub *StreamHub, client *Client, request map[string]interface {}) {
	displays := []Display{}
	err := mapstructure.Decode(request["displays"], &displays)
	if err != nil {
		fmt.Println(err)
		return 
	}
	hub.displays = displays
	get_displays(hub, nil, nil)
}

func get_displays(hub *StreamHub, client *Client, request map[string]interface {}) {
	send_response(hub.notifications, client, "displays", hub.displays)
}

func detect_displays(hub *StreamHub, client *Client, request map[string]interface {}) {
	send := hub.notifications
	go func(){
		send_response(send, client, "displays", displays_detect())
	}()
}

func auto_layout(hub *StreamHub, n_streams int) []Geometry {
	viewports := []Geometry{}

	displays       := hub.displays

	/* find first display with use=true */
	i := 0
	for i=0; i<len(hub.displays); i++ {
		if displays[i].Use { break }
	}

	/* no (enabled) display found? */
	if !(i<len(displays)) {
		hub.viewports = viewports
		return viewports
	}

	x_ofs, y_ofs   := displays[i].Geo.X, displays[i].Geo.Y
	w, h           := displays[i].Geo.W, displays[i].Geo.H
	part           := int(math.Ceil(math.Sqrt(float64(n_streams))))
	w_step, h_step := w/part, h/part

	fmt.Printf("   display[%d] (%s) resolution: %dx%d\n",i,displays[i].Name,w,h)
	fmt.Printf("   n_streams: %d - using %dx%d grid (%dx%d)\n",n_streams,part,part,w_step,h_step)

	/* TODO:
	 * - better layout?
	 * - multi-monitor */

	center_ofs := 0
	for idx := 0; idx < n_streams; idx++ {
		col, row := idx%part, idx/part

		/* center viewports of last row */
		if (col == 0) && ((idx+part)>n_streams) {
			used       := n_streams - idx
			free       := part - used
			center_ofs  = (free * w_step) / 2
		}
		vp := Geometry{
			W : w_step,
			H : h_step,
			X : x_ofs + col*w_step + center_ofs,
			Y : y_ofs + row*h_step,
		}
		viewports = append(viewports, vp)
		//fmt.Println(idx,row,col)
	}

	hub.viewports = viewports
	return viewports
}

func suggest_viewports(hub *StreamHub, client *Client, request map[string]interface {}) {
	tmp, ok := request["n_streams"].(float64)
	if !ok { return }

	n_streams := int(tmp)
	if n_streams < 1 { return }

	viewports := auto_layout(hub, n_streams)
	send      := hub.notifications
	send_response(send, client, "viewports", viewports)
}

/* start playing all streams */
func start_streams_req(hub *StreamHub, client *Client, request map[string]interface {}) {

	if hub.streams_playing { return }

	/* check & adopt stream list */
	streamlist, ok := request["streams"].([]interface{})
	if !ok { return }
	streams := []string{}
	for _, s := range streamlist {
		stream, ok := s.(string)
		if !ok { return }
		streams = append(streams, stream)
	}

	/* check & adopt viewports */
	viewports := []Geometry{}
	mapstructure.Decode(request["viewports"], &viewports)
	if len(viewports) < len(streams) {
		viewports = hub.viewports
	}
	if len(viewports) < len(streams) {
		viewports = auto_layout(hub, len(streams))
	}

	/* check & adopt options */
	options := map[string]bool{}
	//fmt.Println(request["options"])
	mapstructure.Decode(request["options"], &options)

	hub.streams_playing   = true
	hub.stream_locations  = streams
	hub.viewports         = viewports
	hub.playback_options  = options

	global_status(hub, nil, nil) /* signal playing mode to all clients - TODO: add more info */

	/* start players */
	for idx, _ := range hub.stream_locations {
		stream_start(hub, idx)
	}
}

/* stop playing completely */
func stop_streams_req(hub *StreamHub, client *Client, request map[string]interface {}) {

	if !hub.streams_playing { return }

	for idx, _ := range hub.stream_locations {
		stream_stop(hub, idx)
	}

	hub.streams_playing   = false
	global_status(hub, nil, nil) /* signal global stopped mode to all clients */
}

/* start single stream */
func start_stream_req(hub *StreamHub, client *Client, request map[string]interface {}) {
	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream := int(tmp)
	if stream < 0 || stream >= len(hub.stream_locations) { return }

	stream_start(hub, stream)
}

/* stop single stream */
func stop_stream_req(hub *StreamHub, client *Client, request map[string]interface {}) {
	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream := int(tmp)
	if stream < 0 || stream >= len(hub.stream_locations) { return }

	stream_stop(hub, stream)
}

func global_status(hub *StreamHub, client *Client, request map[string]interface {}) {
	note := map[string]interface{}{
		"playing" : hub.streams_playing,
	}
	send_response(hub.notifications, client, "global_status", &note)
}

func stream_ctl_req(hub *StreamHub, client *Client, request map[string]interface {}) {
	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream := int(tmp)
	if stream < 0 || stream >= len(hub.stream_locations) { return }

	ctl, ok := request["ctl"].(string)
	if !ok { return }

	value, ok := request["value"]
	if !ok { return }

	stream_ctl(hub, stream, ctl, value)
}

func get_profiles(hub *StreamHub, client *Client, request map[string]interface {}) {
	send_response(hub.notifications, client, "profiles", hub.stream_profiles)
}

func save_profile(hub *StreamHub, client *Client, request map[string]interface {}) {
	name, ok := request["profile_name"].(string)
	if !ok { return }

	profile, ok := request["profile"].(interface{})
	if !ok { return }

	hub.stream_profiles[name] = profile
	send_response(hub.notifications, nil, "profiles", hub.stream_profiles)
	save_json("stream_profiles.json", hub.stream_profiles)
}

func delete_profile(hub *StreamHub, client *Client, request map[string]interface {}) {
	name, ok := request["profile_name"].(string)
	if !ok { return }

	delete(hub.stream_profiles, name)
	send_response(hub.notifications, nil, "profiles", hub.stream_profiles)
	save_json("stream_profiles.json", hub.stream_profiles)
}

func probe_commands(hub *StreamHub, client *Client, request map[string]interface {}) {
	commands := []string{ "mpv", "yt-dlp", "streamlink" }
	if runtime.GOOS == "linux" {
		commands = append(commands, "xrandr")
	}
	go func(){
		commands_info := []CmdInfo{}
		for _, cmd := range commands {
			commands_info = append(commands_info, probe_command(cmd))
		}
		send_response(hub.notifications, client, "probe_commands", commands_info)
	}()
}

/* handlers are executed in StreamHub.Run() context
 * may access & modify StreamHub XOR start gogoutines as needed */
var req_handlers = map[string]RequestHandler{
	"global_status"      : global_status,
	"probe_commands"     : probe_commands,

	"get_profiles"       : get_profiles,
	"profile_save"       : save_profile,
	"profile_delete"     : delete_profile,

	"detect_displays"    : detect_displays,
	"get_displays"       : get_displays,
	"set_displays"       : set_displays,

	"suggest_viewports"  : suggest_viewports,

	"start_streams"      : start_streams_req,
	"stop_streams"       : stop_streams_req,
	"start_stream"       : start_stream_req,
	"stop_stream"        : stop_stream_req,
	"stream_ctl"         : stream_ctl_req,
}

func client_request(hub *StreamHub, req *ClientRequest) {
	client := req.src
	msg    := req.request
	/* request sanity checking is done here */
	request, ok  := msg["request"].(string)
	if !ok {
		return
	}
	handler, ok := req_handlers[request]
	//fmt.Println("req:", request, req, ok)
	if !ok {
		fmt.Println("client_request: handler missing: ",request)
		return
	}
	handler(hub, client, msg)
}

/* helper function for sending a response to a client */
func send_response(send chan<- *Notification, client *Client, request string, payload interface{}) {
	response := map[string]interface{} {
		"notification"  : request,
		"payload"       : payload,
	}
	json_response, err := json.Marshal(response)
	if err != nil {
		log.Println("send_response JSON Marshal error:", err)
		return
	}
	note := &Notification{
		dst           : client,
		notification  : request,
		payload       : payload,
		json_message  : json_response,
	}
	select {
		case send <- note:
		default:
			log.Fatal("send_response: channel full!")
	}
}
