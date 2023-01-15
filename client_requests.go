package main

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
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
func start_streams(hub *StreamHub, client *Client, request map[string]interface {}) {

	if hub.streams_playing { return }

	// sanitize location
	re := regexp.MustCompile(`[^a-zA-Z0-9-_/:.,?&@=#%]`)

	/* check & adopt stream list */
	streamlist, ok := request["streams"].([]interface{})
	if !ok { return }
	locations := []string{}
	for _, loc := range streamlist {
		location, ok := loc.(string)
		if !ok { return }
		location  = re.ReplaceAllString(location, "")
		if len(location) < 1 { return }
		locations = append(locations, location)
	}

	/* check & adopt viewports */
	viewports := []Geometry{}
	mapstructure.Decode(request["viewports"], &viewports)
	if len(viewports) < len(locations) {
		viewports = hub.viewports
	}
	if len(viewports) < len(locations) {
		viewports = auto_layout(hub, len(locations))
	}

	/* check & adopt options */
	options := map[string]bool{}
	//fmt.Println(request["options"])
	mapstructure.Decode(request["options"], &options)

	hub.streams_playing   = true
	hub.stream_locations  = locations
	hub.viewports         = viewports
	hub.playback_options  = options

	hub.streams           = make([]*Stream, len(hub.stream_locations))

	global_status(hub, nil, nil) /* signal playing mode to all clients - TODO: add more info */

	/* start streams */
	for idx, location := range hub.stream_locations {
		/* build player config */
		mpv_args := []string{
			"--mute=yes",
			"--border=no",
			"--really-quiet",
			"--geometry=" + hub.viewports[idx].String(),
		}
		streamlink_args := []string{
			"--player=mpv",
			"--player-fifo",
			//"-v", // verbose player
		}

		if !options["start_muted"] {
			mpv_args[0] = "--mute=no"
		}

		config := &PlayerConfig{
			mpv_args            : mpv_args,
			location            : location,
			ipc_pipe            : hub.pipe_prefix + strconv.Itoa(idx),
			restart_error_delay : -1,
		}

		if options["restart_error"] {
			config.restart_error_delay = hub.restart_error_delay
		}
		config.restart_user_quit = options["restart_user_quit"]
		config.use_streamlink    = options["use_streamlink"]
		if config.use_streamlink {
			if options["twitch-disable-ads"] {
				streamlink_args = append(streamlink_args, "--twitch-disable-ads")
			}
			config.streamlink_args = streamlink_args
		}

		stream           := NewStream(hub.notifications, idx, config)
		hub.streams[idx]  = stream
		stream.Start()
	}
}

/* stop playing completely */
func stop_streams(hub *StreamHub, client *Client, request map[string]interface {}) {

	if !hub.streams_playing { return }

	for idx, stream := range hub.streams {
		if stream == nil { continue }
		stream.Shutdown()
		hub.streams[idx] = nil
	}

	hub.streams_playing   = false
	global_status(hub, nil, nil) /* signal global stopped mode to all clients */
}

/* start single stream */
func start_stream(hub *StreamHub, client *Client, request map[string]interface {}) {
	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream_idx := int(tmp)
	if stream_idx < 0 || stream_idx >= len(hub.stream_locations) { return }

	stream := hub.streams[stream_idx]
	if stream == nil { return }
	stream.Start()
}

/* stop single stream */
func stop_stream(hub *StreamHub, client *Client, request map[string]interface {}) {
	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream_idx := int(tmp)
	if stream_idx < 0 || stream_idx >= len(hub.stream_locations) { return }

	stream := hub.streams[stream_idx]
	if stream == nil { return }
	stream.Stop()
}

func global_status(hub *StreamHub, client *Client, request map[string]interface {}) {
	note := map[string]interface{}{
		"playing" : hub.streams_playing,
	}
	send_response(hub.notifications, client, "global_status", &note)
}

func stream_ctl(hub *StreamHub, client *Client, request map[string]interface {}) {
	var allowed_ctls = map[string]bool{
		"volume" : true,
		"seek"   : true,
		"mute"   : true,
	}

	if !hub.streams_playing { return }

	tmp, ok := request["stream"].(float64)
	if !ok { return }

	stream_idx := int(tmp)
	if stream_idx < 0 || stream_idx >= len(hub.stream_locations) { return }

	ctl, ok := request["ctl"].(string)
	if !ok { return }

	value, ok := request["value"]
	if !ok { return }

	allowed, ok := allowed_ctls[ctl]
	if (!ok) || (!allowed) { return }

	// sanitize val
	re  := regexp.MustCompile(`[^a-zA-Z0-9]`)
	val := re.ReplaceAllString(fmt.Sprint(value),"")

	stream := hub.streams[stream_idx]
	if stream == nil { return }
	stream.Control(&StreamCtl{cmd:ctl, val:val})
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
	cmd_info := map[string]*CmdInfo{
		"mpv"        : nil,
		"yt-dlp"     : nil,
		"streamlink" : nil,
	}
	if runtime.GOOS == "linux" {
		cmd_info["xrandr"] = nil
	}
	go func(){
		for cmd, _ := range cmd_info {
			cmd_info[cmd] = probe_command(cmd)
		}
		send_response(hub.notifications, client, "probe_commands", cmd_info)
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

	"start_streams"      : start_streams,
	"stop_streams"       : stop_streams,
	"start_stream"       : start_stream,
	"stop_stream"        : stop_stream,
	"stream_ctl"         : stream_ctl,
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
		stream_idx    : -1,
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
