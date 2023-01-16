package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/netdata/go.d.plugin/pkg/iprange"
)

/* StreamHub -> Client */
func ws_Sender(c *Client, conn *websocket.Conn) {
	defer func() {
		conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.client_notify:
			//c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				/* tx channel closed */
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			w.Write([]byte{'\n'})

			// Add queued chat messages to the current websocket message.
			n := len(c.client_notify)
			for i := 0; i < n; i++ {
				w.Write(<-c.client_notify)
				w.Write([]byte{'\n'})
			}

			if err := w.Close(); err != nil {
				return
			}
		}
	}
}

/* Client -> StreamHub */
func ws_Receiver(c *Client, conn *websocket.Conn) {
	defer func() {
		c.shub.Unregister <- c
		close(c.client_request)
		conn.Close()
		fmt.Println("ws closed")
	}()

	for {
		_, rd, err := conn.NextReader()
		if err != nil { break }
		decoder := json.NewDecoder(rd)

		for decoder.More() {
			var msg map[string]interface{}
			if err := decoder.Decode(&msg); err != nil {
				fmt.Println("ws_Receiver JSON decoder:", err)
				break
			}

			fmt.Println("recv:", msg)
			if msg["request"] == nil { continue }
			c.client_request <- msg
		}
	} /* for */
}

var upgrader = websocket.Upgrader{} // use default options

/* start new websock connection */
func serveWs(shub *StreamHub, w http.ResponseWriter, r *http.Request, allowed_ips iprange.Pool) {

	/* allow all if no pool given */
	if allowed_ips != nil {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		allowed  := allowed_ips.Contains(net.ParseIP(ip))
		if !allowed {
			returnCode403(w, r)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("upgrade:", err)
		return
	}

	fmt.Println("ws started")

	client := &Client{
		shub           : shub,

		/* client_notify is written to and closed in StreamHub.Run()
		 * StreamHub will close client_notify after Client sent Unregister */
		client_notify  : make(chan []byte, 256),

		/* client_request is written to and closed in ws_Receiver()
		 * ws_Receiver will close client_request once websock connection is dead */
		client_request : make(chan map[string]interface{}, 256),
	}

	client.shub.Register <- client

	go ws_Receiver(client, conn)
	go ws_Sender(client, conn)
}

func returnCode403(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte("403 Forbidden"))
}

func auth_check(h http.Handler, allowed_ips iprange.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		/* allow all if no pool given */
		if allowed_ips == nil {
			h.ServeHTTP(w, req)
			return
		}

		ip, _, _ := net.SplitHostPort(req.RemoteAddr)
		allowed  := allowed_ips.Contains(net.ParseIP(ip))
		if allowed {
			h.ServeHTTP(w, req)
		} else {
			returnCode403(w, req)
		}
  })
}

func run_webui(shub *StreamHub) {
	var allowed_ips iprange.Pool

	listen_addr := "localhost:8090"
	fmt.Println("webui mode - open this link in your browser: http://"+listen_addr)

	http.Handle("/", auth_check(http.FileServer(http.Dir("./web")), allowed_ips))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(shub, w, r, allowed_ips)
	})
	err := http.ListenAndServe(listen_addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
