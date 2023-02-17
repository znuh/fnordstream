package main

import (
	"flag"
	"fmt"
)

/* TODOs:
 * - web: auto-trigger cmds_modal on missing command?
 * - web: add/show error messages (toasts)
 * - auto-sync for stream buffers
 *
 * - go: OSX monitor detection
 * - layout customisation
 */
func main() {
	fmt.Println("fnordstream v"+version_info)
	no_web          := flag.Bool("no-web", false, "disable webui")
	listen_addr     := flag.String("listen-addr", "localhost:8090", "listen address for web UI")
	webui_acl       := flag.String("allowed-ips", "<ANY>", "allowed IPs for web UI (ranges/netmasks allowed, separate multiple with a comma)")
	allowed_origins := flag.String("allowed-origins", "", "allowed Origins for secondary mode operation (separate multiple with a comma)")
	flag.Parse()

	shub := NewStreamHub()
	go shub.Run()

	if len(flag.Args()) > 0 {
		console_client(shub, flag.Args()[0], *no_web)
	}
	if !(*no_web) {
		webif_run(shub, *listen_addr, *webui_acl, *allowed_origins)
	}
}
