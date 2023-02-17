
const default_port = 8090;

let stream_profiles  = {};
let selected_profile = null;

let tooltipList      = undefined;

let conn_id              = 0;      // connection id counter - increments on connection open
let fnordstreams         = [];     // fnordstreams instances (connections to servers, etc.) by conn_id
let fnordstream_by_peer  = [];     // fnordstream instances by peer

let primary          = undefined;   // primary fnordstream instance

let global           = {            // assembled data from individual fnordstream instances etc.
	url_params       : undefined,

	streams_active   : undefined,
	stream_locations : [],

	/* in multi-host mode displays/viewports have their host_id set to the conn_id
	 * to map them to their fnordstream instances */
	displays         : [],          // flat array of all displays from all fnordstream instances
	viewports        : [],          // flat array of all viewports for all fnordstream instances

	/* collect displays from all fnordstream instances into global.displays (needed by request_viewports())
	 * for non-primary displays display.host_id is set to the conn_id in displays_notification()
	 * this is used to map displays to their fnordstream instances in multi-host mode */
	update_displays : function() {
		this.displays = fnordstreams.flatMap(v => v.displays);
	},

	ws_send         : ws_send,
	streamctl       : global_streamctl,
};

/* displays/viewports handling:
 * 1) detect_displays is sent to each fnordstream instance
 *
 * 2) displays_notification is called w/ displays for each fnordstream reply
 *    - conn_id is added as host_id to each display
 * 2.1) displays_notification calls global.update_displays()
 *    - global list of all displays is rebuilt (displays include host_id (=conn_id))
 * 2.2) displays_notification calls update_displays_table(fnordstream)
 *    - displays table for fnordstream instance is renewed in DOM
 *    - displays and viewports canvases are redrawn as well
 * 2.3) displays_notification calls request_viewports()
 *
 * 3) request_viewports sends global.displays (w/ host_id for each display)
 *    and n_streams to primary fnordstream instance only
 *
 * 4) viewports_notification is triggered with flat list of viewports
 *    viewports are stored in global.viewports as received.
 *    (These viewports have host_id set to the host_id of the display
 *     they're meant for. This host_id is the conn_id of the fnordstream
 *     instance they are meant for.)
 * 4.1) viewports_notification calls assign_streams()
 *
 * 5) assign_streams:
 * 5.1) - clears fnordstream.viewports for all fnordstream instances
 * 5.2) - reassigns global.viewports to fnordstream.viewports
 *        based on the host_id (=conn_id) of each viewport
 * 5.3) - call draw_viewports() do redraw all viewports
 */

/* TODOs:
 * - probe_commands
 * - error messages (unwanted websock close, etc.)
 * - postpone start_streams while viewports_notification pending
 */

function append_option(select,val,txt,selected) {
	var opt = document.createElement("option");
	opt.value       = val;
	opt.textContent = txt;
	opt.selected    = selected;
	select.appendChild(opt);
}

function update_stream_profiles(data) {
	if (data)
		stream_profiles = data;
	const profile_select = document.getElementById('profile_select');
	profile_select.textContent="";
	append_option(profile_select,-1,"New...", selected_profile == null);
	let idx=0;
	for (var key in stream_profiles) {
		append_option(profile_select, idx, key, selected_profile == key);
		idx++;
	}
	profile_select.dispatchEvent(new Event("change"));
}

function gather_options() {
	const gather_list = [
		"use_streamlink",
		"twitch-disable-ads",
		"start_muted",
		"restart_error",
		"restart_user_quit"
	];
	return gather_list.reduce( (res,id) => {
		res[id] = document.getElementById(id).checked;
		return res;
	}, {});
}

function register_handlers() {
	
	/* profile stuff */
	
	const profile_select       = document.getElementById('profile_select');
	const profile_delete       = document.getElementById('profile_delete');
	const profile_name         = document.getElementById('profile_name');
	const profile_save         = document.getElementById('profile_save');
	const profile_viewports_en = document.getElementById('profile_viewports_en');

	profile_delete.addEventListener('click', (event) => {
		primary.ws_send({
			request       : "profile_delete",
			profile_name  : profile_name.value,
		});
		delete stream_profiles[profile_name.value];
		selected_profile = undefined;
		update_stream_profiles();
	});

	/* save profile */
	profile_save.addEventListener('click', (event) => {
		let profile = {
			stream_locations : global.stream_locations,
		}
		if (profile_viewports_en.checked)
			profile.viewports = global.viewports;
		primary.ws_send({
			request       : "profile_save",
			profile_name  : profile_name.value,
			profile       : profile,
		});
		stream_profiles[profile_name.value] = profile;
		selected_profile = profile_name.value;
		update_stream_profiles();
	});

	profile_name.addEventListener('input', (event) => {
	  profile_save.disabled = profile_name.value == "";
	});

	// save/use viewports from profile?
	profile_viewports_en.addEventListener('change', (event) => {
		if (!event.currentTarget.checked)
			global.stream_locations = []; // trigger viewports update in stream_urls.input
		stream_urls.dispatchEvent(new Event("input"));
	});

	/* streams */

	/* start streams */
	const streams_start = document.getElementById('streams_start');
	streams_start.addEventListener('click', (event) => {
		assign_streams(false); // (re)assign most recent stream locations list
		streams_playing(true);
		fnordstreams.forEach(v => {
			if((!v.viewports)||(v.viewports.length<1)) return;
			v.ws_send({                         // send start to all fnordstream instances
				request   : "start_streams",
				streams   : v.stream_locations,
				viewports : v.viewports,
				options   : gather_options(),
			});
		});
	});

	/* stream URLs changed */
	const stream_urls = document.getElementById('stream_urls');
	stream_urls.addEventListener('drop', (event) => {
		const tg = event.target;
		if (tg.value.length > 0)
			tg.value += "\n";
	});
	stream_urls.addEventListener('input', (event) => {
		let vals = event.target.value.split(/\s+/);
		let first = vals.shift();
		if ((first) && (first.length > 0))
			vals.unshift(first);
		let last = vals.pop();
		if ((last) && (last.length > 0))
			vals.push(last);
		const viewports_update = vals.length != global.stream_locations.length;
		global.stream_locations = vals;
		streams_start.disabled = !(global.stream_locations.length > 0);
		// profile_viewports_en.checked?
		if (profile_viewports_en.checked && selected_profile && (stream_profiles[selected_profile].viewports) &&
			stream_profiles[selected_profile].viewports.length >= stream_profiles[selected_profile].stream_locations.length) {
			global.viewports = stream_profiles[selected_profile].viewports;
			assign_streams(true);
		} else if (viewports_update)
			request_viewports();
		//console.log(stream_locations);
	});

	// add new host connection
	const add_host = document.getElementById('add_host');
	add_host.addEventListener('change', (event) => {
		if(event.target === document.activeElement)
			add_connection(event.target.value);
		event.target.value="";
	});

	/* options */

	const streamlink = document.getElementById('use_streamlink');
	const twitch_noads = document.getElementById('twitch-disable-ads');

	streamlink.addEventListener('change', (event) => {
	  if (event.currentTarget.checked) {
		twitch_noads.disabled = false;
	  } else {
		twitch_noads.disabled = true;
		twitch_noads.checked = false;
	  }
	})

	/* Profile select */
	profile_select.addEventListener('change', (event) => {
		let val = profile_select.options[profile_select.selectedIndex].text;
		if (val == "New...")
			val = "";
		//console.log(val);
		profile_delete.disabled = val == "";
		profile_name.disabled   = val != "";
		profile_name.value      = val;
		profile_save.disabled   = profile_name.value == "";
		// profile selected? apply values
		if (!stream_profiles[val]) return;
		selected_profile = val;
		let locs = stream_profiles[selected_profile].stream_locations;
		stream_urls.value = locs.join("\n") + "\n";
		stream_urls.dispatchEvent(new Event("input"));
	})

	/* ********* control pane *************** */

	const streams_mute_all    = document.getElementById('streams_mute_all');
	streams_mute_all.addEventListener('click', ev => global.streamctl("mute", "yes"));

	const streams_stop_all    = document.getElementById('streams_stop_all');
	streams_stop_all.addEventListener('click', ev => global.streamctl("play", "no"));

	const streams_play_all    = document.getElementById('streams_play_all');
	streams_play_all.addEventListener('click', ev => global.streamctl("play", "yes"));

	const streams_ffwd_all    = document.getElementById('streams_ffwd_all');
	streams_ffwd_all.addEventListener('click', ev => global.streamctl("seek", "1"));

	const streams_restart_all = document.getElementById('streams_restart_all');
	streams_restart_all.addEventListener('click', ev => global.streamctl("play", "restart"));

	const streams_quit = document.getElementById('streams_quit');
	streams_quit.addEventListener('click', ev => global.ws_send({request : "stop_streams"}));

	const tooltips_en = document.getElementById('tooltips_enable');
	tooltips_en.addEventListener('change', (event) => {
	  if (!tooltipList) {
		const tooltipTriggerList = document.querySelectorAll('[data-bs-toggle="tooltip"]');
		// TODO: FIXME id="display_info-" class="bi bi-info-circle-fill"
		tooltipList = [...tooltipTriggerList].map(tooltipTriggerEl =>
			tooltipTriggerEl.id != "display-resolution-info" ? new bootstrap.Tooltip(tooltipTriggerEl) : null);
	  }
	  if (event.currentTarget.checked)
		tooltipList.forEach(tt => tt ? tt.enable() : null);
	  else
		tooltipList.forEach(tt => tt ? tt.disable() : null);
	})
}

// OK
function remove_streams() {
	if(!this.streams_tbody) return;
	this.stream_nodes = undefined;
	this.streams_tbody.replaceChildren();
	this.streams_tbody.remove();
	this.streams_tbody = undefined;
}

// OK - setup stream entries for fnordstream instance in streams table
function setup_stream_controls(fnordstream, streams) {
	// discard old tbody - if any
	if (fnordstream.streams_tbody)
		fnordstream.remove_streams();

	const conn_id = fnordstream.conn_id;

	// create tbody
	const tbody_template = document.getElementById('streams_tbody-');
	let tbody            = tbody_template.cloneNode(true);
	tbody.setAttribute('conn_id', conn_id);
	let tbody_nodes      = adapt_nodes([tbody], conn_id);

	tbody_nodes.streams_host_remove.addEventListener('click', (event) =>
		fnordstream.remove());
	tbody_nodes.streams_host_remove.hidden = fnordstream.primary;
	tbody_nodes.streams_host.textContent = "@"+fnordstream.host+":";

	const template = document.getElementById('stream-');
	const ext      = conn_id+".";
	// create stream nodes
	fnordstream.stream_nodes = streams.map( (stream,i) => {
		const url = stream.location;
		let n     = template.cloneNode(true);
		n.hidden  = false;

		let nodes = adapt_nodes([n], ext+i);

		nodes.stream_idx.textContent   = stream.viewport_id != undefined ? stream.viewport_id : i;
		nodes.stream_title.textContent = url;
		nodes.stream_volume.addEventListener('input', (event) => {
			const val = event.target.value;
			fnordstream.streamctl(i,"volume",val);
		});
		nodes.stream_muting.addEventListener('change', (event) => {
			const val = event.target.checked ? "no" : "yes";
			fnordstream.streamctl(i,"mute",val);
		});
		nodes.stream_exclusive_unmute.addEventListener('click', (event) => {
			global.streamctl("mute", "yes", [fnordstream, i]);
			fnordstream.streamctl(i,"mute","no");
		});
		nodes.stream_stop.addEventListener('click',    ev => fnordstream.streamctl(i,"play","no"));
		nodes.stream_play.addEventListener('click',	   ev => fnordstream.streamctl(i,"play","yes"));
		nodes.stream_restart.addEventListener('click', ev => fnordstream.streamctl(i,"play","restart"));
		nodes.stream_ffwd.addEventListener('click',    ev => fnordstream.streamctl(i,"seek","1"));
		tbody.appendChild(n);
		return nodes;
	}); // foreach stream

	// find insertion point
	const table = document.getElementById('streams-table');
	const insrt = Object.values(table.tBodies).find( tb =>
		(tb.getAttribute('conn_id') || Infinity) > conn_id);
	// add tbody to streams table
	table.insertBefore(tbody, insrt);
	fnordstream.streams_tbody = tbody;
}

// OK - assign global streams & viewports to fnordstream instances
function assign_streams(update_viewports) {
	// clear assigned streams and viewports first
	fnordstreams.forEach(v => {
		v.stream_locations = [];
		if(update_viewports != false)
			v.viewports = [];
	});
	const viewports        = global.viewports;
	const stream_locations = global.stream_locations;
	// assign global.viewports and stream_location to fnordstream instances
	viewports.forEach( (vp, idx) => {
		const fnordstream     = fnordstreams[vp.host_id] || primary;
		const stream_location = stream_locations[idx];
		fnordstream.stream_locations.push(stream_location);
		if(update_viewports != false)
			fnordstream.viewports.push(vp);
	});
	if(update_viewports != false)
		draw_viewports();  // redraw viewports
}

// OK
function draw_viewports(fnordstream) {
	/* redraw all if not specified */
	if (!fnordstream) {
		fnordstreams.forEach(v => v.peer ? draw_viewports(v) : null);
		return;
	}
	const viewports = fnordstream.viewports;
	const lightmode = document.getElementById('lightSwitch').checked;
	const cv        = fnordstream.display_nodes.viewports_cv;
	const ctx       = cv.getContext("2d");
	cv.style["mix-blend-mode"] = lightmode ? 'darken' : 'lighten';
	ctx.textAlign = 'center';

	/* clear canvas */
	ctx.fillStyle = lightmode ? '#ffffff' : '#000000';
	ctx.fillRect(0, 0, cv.width, cv.height);

	if((!viewports) || (viewports.length<1))  // nothing to do/draw
		return;

	viewports.forEach( vp => {
		//ctx.fillStyle='#aaaaaa';
		//ctx.fillRect(3.5+vp.x/8, 0.5+vp.y/8, vp.w/8, vp.h/8);
		ctx.strokeStyle = lightmode ? "#000000" : "#ffffff";
		ctx.strokeRect(3.5+vp.x/8, 0.5+vp.y/8, vp.w/8, vp.h/8);

		const font_h  = vp.h/16;
		ctx.fillStyle = lightmode ? "#000000" : "#ffffff";
		ctx.font      = font_h+'px sans-serif';
		ctx.fillText(vp.id, 3.5+vp.x/8 + vp.w/16, (vp.y/8)+(vp.h/16)+(font_h/2)-2, vp.w/8);
	});
}

// OK
function draw_displays(fnordstream) {
	/* redraw all if not specified */
	if (!fnordstream) {
		fnordstreams.forEach(v => v.peer ? draw_displays(v) : null);
		return;
	}
	const lightmode = document.getElementById('lightSwitch').checked;
	const displays  = fnordstream.displays;

	// figure out extents of display(s) setup
	let [w_ext, h_ext] = displays.reduce( (res,d) => {
		const geo = d.geo;
		res[0] = Math.max(res[0], geo.x + geo.w);
		res[1] = Math.max(res[1], geo.y + geo.h);
		return res;
	}, [0,0]);

	//console.log("extents:",w_ext,h_ext);
	w_ext/=8; h_ext/=8;

	const font_h = 20;
	const cv = fnordstream.display_nodes.displays_cv;
	if (!cv) return;
	cv.width  = w_ext+8;
	cv.height = h_ext+font_h*2;

	const ctx = cv.getContext("2d");
	ctx.font = font_h+'px sans-serif';
	ctx.textAlign = 'center';

	ctx.fillStyle = lightmode ? '#ffffff' : '#000000';
	ctx.fillRect(0, 0, cv.width, cv.height);

	displays.forEach( (d,i) => {
		const geo = d.geo;
		ctx.fillStyle = lightmode ? '#dddddd' : '#666666';
		ctx.fillRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.strokeStyle = lightmode ? "#000000" : "#ffffff";
		ctx.strokeRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.fillStyle = lightmode ? "#000000" : "#ffffff";
		ctx.fillText(displays[i].name, 3.5+geo.x/8 + geo.w/16, (geo.y/8)+(geo.h/8)+font_h-2, geo.w/8);
	});

	const cv2 = fnordstream.display_nodes.viewports_cv;
	cv2.width  = w_ext+8;
	cv2.height = h_ext+20;

	draw_viewports(fnordstream);
}

// OK
function set_displays(fnordstream) {
	fnordstream.ws_send({request : "set_displays", displays : fnordstream.displays});
}

// OK - discard old displays table and rebuild
function update_displays_table(fnordstream) {
	const displays = fnordstream.displays;
	const ext      = fnordstream.conn_id+".";
	const target   = fnordstream.display_nodes.display_tbody;
	const template = document.getElementById('display_tr-');

	target.replaceChildren();

	displays.forEach( (d,i) => {
		const geo = d.geo;
		let n = template.cloneNode(true);
		let children = n.childNodes;
		n.id += i;
		n.hidden = false;

		let nodes = adapt_nodes(children, ext+i);
		nodes.display_name.textContent = d.name;
		nodes.display_pos.textContent  = d.geo.x + "," + d.geo.y;
		nodes.display_res.value        = d.geo.w + "x" + d.geo.h;
		nodes.display_res.addEventListener('change', (event) => {
			let val     = event.target.value;
			let wxh     = val.match(/(\d+)x(\d+)$/);
			let short   = val.match(/(\d+)p$/);
			if (wxh) {
				d.geo.w = parseInt(wxh[1]);
				d.geo.h = parseInt(wxh[2]);
			} else if (short) {
				let h   = parseInt(short[1]);
				let w   = h * 16;
				if (w%9 == 0) {
					d.geo.h = h;
					d.geo.w = Math.floor(w/9);
				}
			}
			event.target.value = d.geo.w + "x" + d.geo.h;
			draw_displays(fnordstream);
			set_displays(fnordstream);
		});
		nodes.display_use.checked = d.use;
		nodes.display_use.addEventListener('change', (event) => {
			d.use = event.target.checked;
			set_displays(fnordstream);
		});

		target.appendChild(n);
	}); /* foreach display */

	draw_displays(fnordstream);
}

// OK
function request_viewports() {
	// short-circuit requests for 0 streams
	if(global.stream_locations.length<1) {
		global.viewports = [];
		assign_streams(true);
		return;
	}
	primary.ws_send({
		request   : "suggest_viewports",
		n_streams : global.stream_locations.length,
		displays  : global.displays,
		discard   : true
	});
}

// OK
function displays_notification(fnordstream, msg) {
	let v = msg.payload
	if ((!v) || (v.length < 1)) {
		const toast = new bootstrap.Toast(document.getElementById('displaydetect_failed'));
		toast.show();
		return;
	}
	const conn_id = fnordstream.conn_id;
	if(conn_id>0)
		v.forEach(v => v.host_id = conn_id);  // add host_id to displays
	fnordstream.displays = v;
	global.update_displays();
	update_displays_table(fnordstream);
	request_viewports();
}

// OK
function viewports_notification(fnordstream, msg) {
	if(!fnordstream.primary) return;
	v = msg.payload
	if ((!v) || (v.length < 1))
		v = [];
	global.viewports = v;
	assign_streams(true);
}

// OK
function mpv_property_changed(fnordstream, property, stream_id) {
	const property_map = { /* property -> node_name map */
		"media-title"            : "stream_title",
		"demuxer-cache-duration" : "stream_buffer",
		"mute"                   : "stream_muting",
		"volume"                 : "stream_volume",
		"video-bitrate"          : "stream_vbr",
	};
	const update_funcs = { /* node_name -> update function map */
		"stream_title"  : (n, v) => n.textContent = v,
		"stream_buffer" : (n, v) => {
				let str = Math.round(v*10)/10 + "";
				str += (str.indexOf(".")>=0) ? "" : ".0";
				n.textContent = "Buffer: " + str + "s";
			},
		"stream_vbr" : (n, v) => {
				let str = Math.round(v/100000)/10 + "";
				str += (str.indexOf(".")>=0) ? "" : ".0";
				n.textContent = "VBR: " + str + "Mb/s";
			},
		"stream_volume" : (n, v) => n.value       = v,
		"stream_muting" : (n, v) => n.checked     = !v,
	};

	let target = property_map[property.name];
	if (!target) {
		//console.log("no node name", stream_id, target, property);
		target = property.name;
		//return;
	}

	node = fnordstream.stream_nodes[stream_id][target];
	if (!node) {
		//console.log("no target node", stream_id, target, property);
		return;
	}

	let val = property.data;
	if ((val == null) || (val == undefined))
		return;

	let update_func = update_funcs[target];
	if (!update_func) {
		console.log("no update func", stream_id, target, property);
		return;
	}
	update_func(node,val);
}

// OK
function player_event(fnordstream, msg) {
	const event = msg.payload;

	if ((!event) || (event.length < 1) || (!fnordstream.stream_nodes))
		return;

	stream_id = parseInt(msg.stream_id);
	if (isNaN(stream_id) || (stream_id >= fnordstream.stream_nodes.length) || (stream_id < 0))
		return;

	if (event.event == "property-change") {
		//console.log(stream_id, event);
		mpv_property_changed(fnordstream, event, stream_id);
	}
	//else
		//console.log(stream_id, event, msg);
}

// OK
function player_status(fnordstream, msg) {
	const payload = msg.payload;

	if ((!payload) || (payload.length < 1) || (!fnordstream.stream_nodes))
		return;

	stream_id = parseInt(msg.stream_id);
	if (isNaN(stream_id) || (stream_id >= fnordstream.stream_nodes.length) || (stream_id < 0))
		return;

	const status = payload.status;
	const stream_nodes = fnordstream.stream_nodes;

	stream_nodes[stream_id].stream_volume.disabled              = status != "playing";
	stream_nodes[stream_id].stream_buffer.disabled              = status != "playing";
	stream_nodes[stream_id].stream_muting.disabled              = status != "playing";
	stream_nodes[stream_id].stream_exclusive_unmute.disabled    = status != "playing";
	stream_nodes[stream_id].stream_stop.disabled                = (status == "stopped") || (status == "stopping");
	stream_nodes[stream_id].stream_play.disabled                = (status != "stopped") && (status != "stopping");
	stream_nodes[stream_id].stream_ffwd.disabled                = status != "playing";

	stream_nodes[stream_id].stream_playing.hidden               = status != "playing";
	stream_nodes[stream_id].stream_stopped.hidden               = (status != "stopped") && (status != "stopping");
	stream_nodes[stream_id].stream_starting.hidden              = (status != "starting") && (status != "restarting");

	//console.log("player_status", stream_id, status);
}

// OK - global playing status
function streams_playing(active) {
	active = active || fnordstreams.some(v => v.playing);
	if (active == global.streams_active) return;
	global.streams_active = active;

	document.getElementById('setup-tab').disabled   = false;
	document.getElementById('control-tab').disabled = false;

	if (active)
		document.getElementById('control-tab').dispatchEvent(new Event("click"));
	else
		document.getElementById('setup-tab').dispatchEvent(new Event("click"));

	document.getElementById('setup-tab').disabled   =  active;
	document.getElementById('control-tab').disabled = !active;
}

// OK
function global_status(fnordstream, msg) {
	const status = msg.payload;

	if ((!status) || (status.length < 1))
		return;

	if(fnordstream.primary)
		document.title = "fnordstream v"+status.version+" @"+window.location.hostname;

	fnordstream.playing = status.playing;
	streams_playing(status.playing);

	if (!status.playing) {
		if(fnordstream.streams_tbody)      // remove streams from table
			fnordstream.remove_streams();
		return;
	}

	const streams = status.streams;

	/* create stream nodes */
	setup_stream_controls(fnordstream, streams);

	/* set status and properties for all streams */
	streams.forEach( (stream,i) => {
		player_status(fnordstream, {"stream_id":i, "payload":{"status":stream.player_status}});
		if (!stream.properties)
			return;
		for (const [key, value] of Object.entries(stream.properties)) {
			//console.log(key,value);
			player_event(fnordstream, {"stream_id":i, "payload":{
				"event" : "property-change",
				"name"  : key,
				"data"  : value,
			}});
		} // foreach property
	});
}

const required_commands = {
	"mpv"        : true,
	"yt-dlp"     : true,
};

function mklink(ref) {
	const refs = {
		"mpv"        : "https://mpv.io/installation/",
		"yt-dlp"     : "https://github.com/yt-dlp/yt-dlp#installation",
		"streamlink" : "https://streamlink.github.io/install.html",
		"xrandr"     : "https://xorg-team.pages.debian.net/xorg/howto/use-xrandr.html",
	};
	return refs[ref] ? ('<a href="'+refs[ref]+'">'+ref+'</a>') : ref;
}

// OK
function populate_cmds_table(fnordstream) {
	const cmds     = fnordstream.cmds_info;
	const host     = fnordstream.host;
	const template = document.getElementById('cmd-');
	const parent   = template.parentNode;
	parent.replaceChildren(template);

	const host_node       = document.getElementById('cmds_modal_host');
	host_node.textContent = "/ @" + fnordstream.host;

	const sorted = ["mpv", "yt-dlp", "streamlink", "xrandr"];

	// populate commands table
	for (let i=0;i<sorted.length;i++) {
		const n = template.cloneNode(true);
		const children = n.childNodes;
		n.id += i;
		n.hidden = false;

		const cmd_name = sorted[i];
		const cmd      = cmds[cmd_name];
		if (!cmd) continue;

		const code     = parseInt(cmd.exit_code);
		const required = required_commands[cmd_name];

		n.classList.add( (code == 0) ? "table-success" : (required ? "table-danger" : "table-warning"));

		const nodes = adapt_nodes(children, i);
		nodes.cmd_required.textContent = required ? "required" : "optional";
		nodes.cmd_cmd.innerHTML        = "<b>" + mklink(cmd_name) + "</b>";
		nodes.cmd_exitcode.innerHTML   = code ?
			"<b>" + cmd.exit_code + "&#x2718;</b>" : cmd.exit_code + "&#x2714;";
		nodes.cmd_output.textContent   = cmd.error ? cmd.error : cmd.stdout;

		parent.appendChild(n);
	}
}

// TBD: modal for command details table
function commands_probed(fnordstream, msg) {

	const results = msg.payload;
	if ((!results) || (results.length < 1))
		return;

	fnordstream.cmds_info = results;

	const conn_id = fnordstream.conn_id;

	/* disable/enable use_streamlink switch */
	const use_streamlink   = document.getElementById('use_streamlink');
	const streamlink_note  = document.getElementById('streamlink_note');
	// iterate over all fnordstream instances, look out for missing streamlink
	let missing_streamlink = fnordstreams.find(f =>
		(f.cmds_info) &&
		((!f.cmds_info["streamlink"]) || (f.cmds_info["streamlink"].exit_code != 0))
	);
	// disable streamlink option if missing on any host
	use_streamlink.disabled     = missing_streamlink != undefined;
	streamlink_note.textContent = missing_streamlink ?
		"No working streamlink found on "+missing_streamlink.host+"." : "";

	/* walk through list to check mandatory & optional commands */
	let missing_required = "";
	let missing_optional = "";
	Object.keys(results).forEach(function(cmd, index) {
		const v = results[cmd];
		if (required_commands[cmd]) {
			if (v.exit_code != 0)
				missing_required += ", " + mklink(cmd);
		} else {
			if (v.exit_code != 0)
				missing_optional += ", " + mklink(cmd);
		}
		//console.log(cmd, v);
	});
	missing_required = missing_required.substring(2);
	missing_optional = missing_optional.substring(2);

	/* build status note */
	const cmd_status = document.getElementById('cmd_status');
	const template   = document.getElementById('cmds_alert-');
	const alert      = template.cloneNode(true);
	let   nodes      = adapt_nodes([alert], conn_id);
	alert.hidden     = false;

	let alert_type = 'success';
	let alert_msg = "All required &amp; optional commands found.";
	if (missing_required.length > 0) {
		alert_type = 'danger';
		alert_msg = "<b>Required commands failed: " + missing_required + "</b><br>";
	}
	if (missing_optional.length > 0) {
		if (alert_type == "success") {
			alert_type = 'warning';
			alert_msg = "Optional commands failed: " + missing_optional;
		}
		else
			alert_msg += "Optional commands failed: " + missing_optional;
	}
	nodes.cmds_alert.classList.add("alert-"+alert_type);
	nodes.cmds_alert_host.textContent = "@"+fnordstream.host+":";
	nodes.cmds_alert_msg.innerHTML    = alert_msg;

	if(!fnordstream.cmds_alert)
		cmd_status.appendChild(alert);
	else
		fnordstream.cmds_alert.replaceWith(alert);
	fnordstream.cmds_alert = alert;

	/* button handlers for status note */
	const commands_refresh = nodes.commands_refresh;
	commands_refresh.disabled = false;
	if (!commands_refresh.getAttribute('click_attached')) {
		commands_refresh.addEventListener('click', refresh_cmds);
		commands_refresh.setAttribute('click_attached', 'true');
	}

	nodes.commands_details.addEventListener('click', evt =>
		populate_cmds_table(fnordstream));

/*
	const commands_refresh2 = document.getElementById('commands_refresh2');
	commands_refresh2.disabled = false;
	if (!commands_refresh2.getAttribute('click_attached')) {
		commands_refresh2.addEventListener('click', refresh_cmds);
		commands_refresh2.setAttribute('click_attached', 'true');
	}
*/

	const cmd_refresh_busy = nodes.cmd_refresh_busy;
//	const cmd_refresh_busy2 = document.getElementById('cmd_refresh_busy2');
//	cmd_refresh_busy2.hidden = true;

	const req_missing = document.getElementById('req_missing');
	req_missing.hidden = !(missing_required.length > 0);
/*
	const close_btn1 = document.getElementById('cmd_modal_close1');
	const close_btn2 = document.getElementById('cmd_modal_close2');
	close_btn1.disabled = missing_required.length > 0;
	close_btn2.disabled = missing_required.length > 0;

	if (missing_required.length > 0) {
		cmd_modal = cmd_modal || new bootstrap.Modal(document.getElementById('cmds_modal'))
		cmd_modal.show();
	}
*/
	function refresh_cmds() {
		fnordstream.ws_send({request : "probe_commands"});
		commands_refresh.disabled = true;
		//commands_refresh2.disabled = true;
		nodes.cmd_refresh_busy.hidden = false;
		//cmd_refresh_busy2.hidden = false;
	}

	//populate_cmds_table(results);
}

// OK
function profiles_notification(fnordstream, msg) {
	if (!fnordstream.primary) return;
	const profiles = msg.payload;
	update_stream_profiles(profiles);
}

const ws_handlers = {
	"global_status"  : global_status,
	"probe_commands" : commands_probed,
	"profiles"       : profiles_notification,
	"displays"       : displays_notification,
	"viewports"      : viewports_notification,
	"player_event"   : player_event,
	"player_status"  : player_status,
};

// OK
function adapt_nodes(nodelist, new_extension, node_table) {
	if ((!nodelist) || (nodelist.length < 1))
		return;
	let res = node_table || [];
	nodelist.forEach(n => {
		// adapt for-tags as well
		if (n.attributes && n.attributes["for"] && (n.attributes["for"].value.slice(-1) == "-"))
			n.attributes["for"].value += new_extension;

		// adapt IDs
		if (n.id && (n.id.slice(-1) == "-")) {
			res[n.id.slice(0,-1)] = n;
			n.id += new_extension;
		}

		adapt_nodes(n.childNodes, new_extension, res);
	});
	return res;
}

// OK
function remove_displays() {
	if(!this.display_table) return;
	this.display_nodes = undefined;
	this.display_table.replaceChildren();
	this.display_table.remove();
	this.display_table = undefined;
}

// OK
function create_displays(fnordstream) {
	const conn_id = fnordstream.conn_id;
	const server  = fnordstream.host;
	let template  = document.getElementById('display_table-');
	let parent    = template.parentNode;

	let n = template.cloneNode(true);
	let children = n.childNodes;
	n.id += conn_id;
	n.hidden = false;

	let nodes = adapt_nodes(children, conn_id);
	nodes.display_table = n;

	nodes.display_host_remove.addEventListener('click', (event) =>
		fnordstream.remove());
	nodes.display_host_remove.hidden = fnordstream.primary;
	nodes.refresh_displays.addEventListener('click', (event) =>
		fnordstream.ws_send({request:"detect_displays"}));
	nodes.display_host.textContent = "@"+server+":";

	const info_tt = new bootstrap.Tooltip(nodes.display_info);

	fnordstream.display_nodes = nodes;
	fnordstream.display_table = n;

	parent.appendChild(n);
	//nodes.refresh_displays.dispatchEvent(new Event("click"));
}

// OK
function ws_send(requests, exempt) {
	requests = array(requests);
	const buf = requests.reduce((res,v) => v ? res+JSON.stringify(v) : res, "");
	if(buf.length<=2) return;
	if(this == global)
		fnordstreams.forEach(v => (v == exempt) || v.websock.send(buf));
	else
		this.websock.send(buf);
}

// OK
function streamctl(stream_id,ctl,val) {
	this.websock.send(JSON.stringify({
		request   : "stream_ctl",
		stream_id : stream_id,
		ctl       : ctl,
		value     : val
	}));
}

// OK
function global_streamctl(ctl, val, exempt) {
	const [ex_fns, ex_id] = exempt ? exempt : [];
	let ex_msg = exempt ? JSON.stringify({
		request   : "stream_ctl",
		stream_id : "!"+ex_id,
		ctl       : ctl,
		value     : val
	}) : null;
	let msg = JSON.stringify({
		request   : "stream_ctl",
		stream_id : "*",
		ctl       : ctl,
		value     : val
	});
	fnordstreams.forEach(fs => fs.websock.send(fs != ex_fns ? msg : ex_msg));
}

function fnordstream_remove() {
	// delete host from list in url_params
	const host = this.host;
	const port = this.port;
	global.url_params.add_hosts??=[];
	const len_pre = global.url_params.add_hosts.length;
	global.url_params.add_hosts = global.url_params.add_hosts.filter( v =>
		!((v == host+":"+port) || ((v==host)&&(port==default_port))) );
	if(global.url_params.add_hosts.length != len_pre)
		url_encode_params();
	this.websock.close();
}

function add_connection(dst, add_to_url) {
  const port_match = dst.match(/((?::))(?:[0-9]+)$/);
  const host = port_match ? dst.substring(0,port_match.index) : dst;
  const port = port_match ? port_match[0].substring(1) : default_port;
  const peer = host+":"+port;

  if (fnordstream_by_peer[peer]) return; // check for duplicate connections

  let fnordstream = null;
  let websock     = null;
  try {
	  websock = new WebSocket("ws://"+peer+"/ws");
  }
  catch(err) {
	  console.log(err);
	  return;
  }

  fnordstream_by_peer[peer] = true;

  websock.addEventListener('open', (event) => {
	fnordstream = {
		host           : host,
		port           : port,
		peer           : peer,
		websock        : websock,
		conn_id        : conn_id,

		remove         : fnordstream_remove,
		ws_send        : ws_send,
		streamctl      : streamctl,

		displays       : undefined,
		viewports      : undefined,

		playing        : undefined,
		display_nodes  : undefined,  // nodes for display table
		display_table  : undefined,  // table holding displays

		streams_tbody  : undefined,  // tbody in streams table
		stream_nodes   : undefined,  // control/indicator nodes for streams

		cmds_info      : undefined,  // list of required/missing commands
		cmds_alert     : undefined,  // alert box for missing commands

		remove_displays : remove_displays,
		remove_streams  : remove_streams,
	};
	fnordstreams[conn_id++]   = fnordstream;
	fnordstream_by_peer[peer] = fnordstream;
	fnordstream.primary = fnordstream.conn_id == 0;
	if(fnordstream.primary)
		primary = fnordstream;

	// create displays list for host
	create_displays(fnordstream);

	// send initial requests
	fnordstream.ws_send([
		{request : "detect_displays"},
		{request : "global_status"},
		fnordstream.primary ? {request : "get_profiles"} : undefined,
		{request : "probe_commands"}
	]);

    document.getElementById('stream_urls').dispatchEvent(new Event("input"));
    console.log("websock opened",fnordstream.conn_id,peer);

    if((add_to_url == false)||fnordstream.primary)
		return;

	// add host to URL params if not yet in there
	global.url_params.add_hosts??=[];
	if( ((port==default_port)&&(global.url_params.add_hosts.includes(host))) ||
	    global.url_params.add_hosts.includes(host+":"+port) )
	    return;
	global.url_params.add_hosts.push(port==default_port ? host : host+":"+port);
	url_encode_params();
  });

  websock.addEventListener('message', (evt) => {
	  evt.data.split('\n').forEach( s => {
		  if (s.length < 2) return;
		  const msg = JSON.parse(s);
		  //console.log(msg);
		  if (msg.notification && ws_handlers[msg.notification])
			ws_handlers[msg.notification](fnordstream, msg);
		else
			console.log(msg);
	  }); // forEach
  });

  websock.addEventListener('close', (event) => {
	  if(!fnordstream) return;
	  // cleanup nodes
	  fnordstream.remove_displays();
	  fnordstream.remove_streams();
	  if(fnordstream.cmds_alert) {
		  fnordstreams.cmds_alert.replaceChildren();
		  fnordstreams.cmds_alert.remove();
		  fnordstreams.cmds_alert = undefined;
		  // TODO: update streamview dependency
	  }
	  // TODO: cleanup commands modal if active
	  const id = fnordstream.conn_id;
	  const update_viewports = fnordstream.viewports && (fnordstream.viewports.length>0);
	  delete(fnordstreams[id]);
	  delete(fnordstream_by_peer[fnordstream.peer]);
	  fnordstream = undefined;
	  global.update_displays();
	  if(update_viewports)
		request_viewports();
	  console.log("websock closed", id,peer);
  });
}

function url_encode_params() {
	const res = Object.entries(global.url_params).flatMap( ([key, value]) => {
		if(value == null) return;
		const va = array(value);
		return va.length > 0 ? key+"="+va.join(",") : null;
	}).filter(v => v!=null).join(";");
	let new_url = window.location.href.match(/^[^#]+/)[0];
	new_url += res.length > 0 ? "#"+res : "";
	window.history.replaceState(null, '', new_url);
}

function url_decode_params() {
	const hash = window.location.hash.match(/^#(.*)/);
	if(!hash) return [];
	const parts = hash[1].split(';');
	return parts.reduce( (res,p) => {
		const [k,v] = p.split('=');
		if((k != undefined) && (k!=""))
			res[k] = v ? v.split(',') : [];
		return res;
	},[]);
}

document.addEventListener("DOMContentLoaded", function() {
  add_connection(window.location.host, false);
  register_handlers();

  // parse additional URL params (if any)
  global.url_params = url_decode_params();

  const add_hosts   = global.url_params.add_hosts || [];
  add_hosts.forEach(h => add_connection(h, false));
});

function array(x) { // simple helper
	return Array.isArray(x) ? x : [x];
}
