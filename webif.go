package main

import (
	"fmt"
	"log"
	"strings"
	"io/fs"
	"net"
	"net/url"
	"net/http"
	"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/netdata/go.d.plugin/pkg/iprange"
)

type WSConfig struct {
	acl                 iprange.Pool
	allowed_origins     map[string]bool
}

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

			//fmt.Println("recv:", msg)
			if msg["request"] == nil { continue }
			c.client_request <- msg
		}
	} /* for */
}

var upgrader = websocket.Upgrader{} // use default options

/* start new websock connection */
func serveWs(shub *StreamHub, w http.ResponseWriter, r *http.Request, cfg *WSConfig) {

	if !auth_check(w, r, cfg.acl) {
		return
	}

	if cfg.allowed_origins != nil {
		upgrader.CheckOrigin = func(req *http.Request) bool {
			return origin_check(req, cfg.allowed_origins)
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("upgrade:", err)
		return
	}

	fmt.Println("ws started for " + r.RemoteAddr)

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

func origin_check(req *http.Request, allowed_origins map[string]bool) bool {
	origin := req.Header["Origin"]
	if len(origin) == 0 { return true }
	u, err := url.Parse(origin[0])
	if err != nil { return false }
	allowed := (u.Host == req.Host) || allowed_origins[u.Host]
	if !allowed { log.Println("Origin",u.Host,"not allowed") }
	return allowed
}

func auth_check(w http.ResponseWriter, req *http.Request, acl iprange.Pool) bool {
	if acl == nil {  /* allow all if ACL is nil */
		return true
	}
	ip, _, _ := net.SplitHostPort(req.RemoteAddr)
	allowed  := acl.Contains(net.ParseIP(ip))
	if !allowed {
		log.Println("client",ip,"not authorized in whitelist")
		returnCode403(w, req)
	}
	return allowed
}

func auth_wrap(h http.Handler, cfg *WSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if auth_check(w, req, cfg.acl) {
			h.ServeHTTP(w, req)
		}
  })
}

func webif_run(shub *StreamHub, listen_spec string, webui_acl string, allowed_origins string) {
	cfg := &WSConfig{}   //acl iprange.Pool  default: nil (ALLOW ALL)

	log.SetFlags(0)

	fmt.Println("===== webui mode =====")

	// parse listen address
	listen_host, listen_port, err := net.SplitHostPort(listen_spec)
	if err != nil {
		log.Fatal("ERROR: invalid listen ", err)
	}
	listen_addr := listen_host + ":" + listen_port
	fmt.Println("listen address :", listen_addr)

	/* parse client whitelist (if given)
	   if no client whitelist is provided *ALL* clients will be allowed! */
	if webui_acl != "<ANY>" {
		ranges := strings.ReplaceAll(webui_acl, ",", " ")
		cfg.acl, err = iprange.ParseRanges(ranges)
		if err != nil {
			log.Fatal("ERROR: ", err)
		}
		if cfg.acl == nil { // make empty string result in empty range instead of nil
			cfg.acl = []iprange.Range{}
			fmt.Println("allowed clients:", "*NONE*", "- very nobody - many blocked - wow!")
		} else {
			fmt.Println("allowed clients:", cfg.acl)
		}
	}

	// smack user if they attempt to start non-localhost server without restricting access through -allowed-ips
	if (listen_host != "127.0.0.1") && (listen_host != "localhost") && (cfg.acl == nil) {
		fmt.Println("allowed clients:", "*ANY*")
		str := "ERROR: I'm sorry Dave, I'm afraid I can't do that.\n"
		str += "       For a non-localhost listen address you *MUST* provide a list of allowed clients with -allowed-ips."
		log.Fatal(str)
	}

	// assemble map of allowed Origins
	if allowed_origins != "" {
		list := strings.Split(allowed_origins,",")
		cfg.allowed_origins = make(map[string]bool)
		for _, v := range list {
			if v != "" { cfg.allowed_origins[v] = true }
		}
		fmt.Println("allowed origins:",list)
	}

	// serve embedded webfs or web/ directory?
	var web_fs http.FileSystem
	if(embedded_webfs_valid) {
		fmt.Println("serving embedded webfs")
		fsys       := fs.FS(embedded_webfs)
		webdir, _  := fs.Sub(fsys, "web")
		web_fs      = http.FS(webdir)
	} else {
		fmt.Println("serving web/ directory")
		web_fs      = http.Dir("./web")
	}

	http.Handle("/", auth_wrap(http.FileServer(web_fs), cfg))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(shub, w, r, cfg)   // websocket
	})

	fmt.Println("open this link in your browser: http://localhost:"+listen_port)

	err = http.ListenAndServe(listen_addr, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
