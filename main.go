package main

import "flag"

/* TODOs:
 * - web: add dark mode
 * - go: extend global status on connect (include stream info, etc.)
 * - web: add stream status indicator
 * - go: switch stream to user_intent + FSM?
 * - auto-sync for stream buffers
 *
 * - go: OSX monitor detection
 * - layout customisation (incl. multi-monitor)
 * - web: add support for remote displays
 * - go: add support for remote clients (w/ IP/user auth?)
 */
func main() {
	no_web := flag.Bool("no-web", false, "disable webui")
	flag.Parse()

	shub := NewStreamHub()
	go shub.Run()

	if len(flag.Args()) > 0 {
		console_client(shub, flag.Args()[0], *no_web)
	}
	if !(*no_web) {
		run_webui(shub)
	}
}
