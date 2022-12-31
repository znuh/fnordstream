package main

import (
	"fmt"
	"log"
	"net/http"
	"encoding/json"

	"github.com/gorilla/websocket"
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
func serveWs(shub *StreamHub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}

	log.Printf("ws started\n")

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

func run_webui(shub *StreamHub) {
	listen_addr := "localhost:8090"
	fmt.Println("webui mode - open this link in your browser: http://"+listen_addr)

	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(shub, w, r)
	})
	err := http.ListenAndServe(listen_addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
