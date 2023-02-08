package main

import (
	//"fmt"
	"time"
	"runtime"
	"strconv"
)

type Client struct {
	shub            *StreamHub
	client_notify    chan []byte
	client_request   chan map[string]interface{}
}

type ClientRequest struct {
	src               *Client
	request            map[string]interface{}
}

type Notification struct {
	dst               *Client
	stream_id          int       // stream index source (if >= 0)

	notification       string
	payload            interface{}

	json_message     []byte
}

type StreamStatus struct {
	Player_status      string                    `json:"player_status"`
	Location          *string                    `json:"location,omitempty"`
	Viewport_id        int                       `json:"viewport_id"`
	Properties         map[string]interface{}    `json:"properties,omitempty"`
}

type StreamHub struct {
	// register/unregister channels for clients
	Register              chan *Client
	Unregister            chan *Client

	// registered clients
	clients               map[*Client]bool

	client_requests       chan *ClientRequest     // channel holding multiplexed requests of all clients (fan-in)
	notifications         chan *Notification      // channel holding multiplexed notifications for all clients (fan-out)

	/* streams stuff */
	displays            []Display
	viewports           []Viewport
	stream_locations    []string
	playback_options      map[string]bool

	streams_playing       bool
	streams             []*Stream
	stream_status       []*StreamStatus

	pipe_prefix           string
	restart_error_delay   time.Duration

	stream_profiles       map[string]interface{}
}

func NewStreamHub() *StreamHub {
	shub := &StreamHub{
		Register            : make(chan *Client),
		Unregister          : make(chan *Client),

		clients             : make(map[*Client]bool),
		client_requests     : make(chan *ClientRequest, 64),
		notifications       : make(chan *Notification, 64),

		displays            : displays_detect(),
		pipe_prefix         : "/tmp/nstream_mpv_ipc",
		restart_error_delay : 1*time.Second,
	}
	if runtime.GOOS == "windows" {
		shub.pipe_prefix = "\\\\.\\pipe\\nstream_mpv_ipc"
	}
	shub.stream_profiles = map[string]interface{} {}
	load_json("stream_profiles.json", &shub.stream_profiles)
	return shub
}

func mux_client(hub *StreamHub, client *Client) {
	for {
		msg, ok := <- client.client_request
		//fmt.Println("mux_client",ok,msg)
		if !ok { break }
		hub.client_requests <- &ClientRequest{src : client, request: msg}
	}
	//fmt.Println("mux_client done")
}

func try_forward(client *Client, message []byte) {
	select {
		case client.client_notify <- message:
		default:
			/* client channel full - drop message */
	}
}

/* Responses to individual clients (non-broadcast) are also sent through the 
 * client_notifies channel of the hub and forwarded to the client by StreamHub.Run()
 * The benefit of this approach is that writes to the client notify channel and 
 * closing this channel only happen in StreamHub.Run().
 * 
 * Therefor client_request() may start go routines to handle certain requests
 * and send the response via the multiplexed client_notifies channel of the StreamHub. */
func (hub * StreamHub) Run() {
	for {
		select {

			/* client register */
			case client := <-hub.Register:
				//fmt.Println("register client")
				hub.clients[client] = true
				go mux_client(hub, client)

			/* client unregister */
			case client := <-hub.Unregister:
				if _, ok := hub.clients[client]; ok {
					delete(hub.clients, client)
					if client.client_notify != nil {
						close(client.client_notify)
					}
				}

			/* client requests - includes client -> player messages */
			case req := <-hub.client_requests:
				client_request(hub, req)

			/* messages to clients - includes player -> client messages */
			case note := <-hub.notifications:
				client       := note.dst
				json_message := note.json_message

				// prepend JSON data with note type and stream_id
				if note.stream_id >= 0 {
					prepend          := `{"notification":"`+note.notification+`","stream_id":`+strconv.Itoa(note.stream_id)+`,"payload":`
					str              := prepend + string(json_message) + "}"
					json_message      = []byte(str)
					note.json_message = json_message
				}

				/* watch notification and follow certain state/value changes */
				notification(hub, note)

				if client == nil {                             /* broadcast to all clients */
					for client := range hub.clients {
						try_forward(client, json_message)
					}
				} else if _, ok := hub.clients[client]; ok {    /* single client only */
					try_forward(client, json_message)
				}
		} /* select */
	} /* for */
}
