
let stream_profiles  = {};
let selected_profile = null;

let tooltipList      = undefined;

let conn_id          = 0;           // connection id counter - increments on connection open
let fnordstreams     = {};          // fnordstreams instances (connections to servers, etc.)
let primary          = undefined;   // primary fnordstream instance

// TODO: global playing status
let global           = {            // assembled data from individual fnordstream instances
	streams_active   : undefined,
	stream_locations : [],
	displays         : [],
	viewports        : [],

	update_displays : function() {
		this.displays = Object.values(fnordstreams).flatMap(v => v.displays);
	}
};

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
		const options = gather_options();
		streams_playing(true);
		Object.values(fnordstreams).forEach(v => {
			if((!v.viewports)||(v.viewports.length<1)) return;
			v.ws_send({                         // send start to all fnordstream instances
				request   : "start_streams",
				streams   : v.stream_locations,
				viewports : v.viewports,
				options   : options
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
			assign_viewports();
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
	streams_mute_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "mute", "yes");
	})

	const streams_stop_all    = document.getElementById('streams_stop_all');
	streams_stop_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "play", "no");
	})

	const streams_play_all    = document.getElementById('streams_play_all');
	streams_play_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "play", "yes");
	})

	const streams_ffwd_all    = document.getElementById('streams_ffwd_all');
	streams_ffwd_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "seek", "1");
	})

	const streams_restart_all = document.getElementById('streams_restart_all');
	streams_restart_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "play", "restart");
	})

	const streams_quit = document.getElementById('streams_quit');
	streams_quit.addEventListener('click', (event) => {
		ws_send({request : "stop_streams"});
		//streams_playing(false);
	});

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

function setup_stream_controls(fnordstream, streams) {
	let template = document.getElementById('stream-');
	let parent   = template.parentNode;
	const ext    = fnordstream.conn_id+".";
	parent.replaceChildren(template);

	fnordstream.stream_nodes = streams.map( (stream,i) => {
		const url = stream.location;
		let n = template.cloneNode(true);
		let children = n.childNodes;
		n.id += i;
		n.hidden = false;

		let nodes = adapt_nodes(children, ext+i);

		nodes.stream_idx.textContent   = i;
		nodes.stream_title.textContent = url;
		nodes.stream_volume.addEventListener('input', (event) => {
			const val = event.target.value;
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "volume",
				value       : val
			});
		});
		nodes.stream_muting.addEventListener('change', (event) => {
			const val = event.target.checked ? "no" : "yes";
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "mute",
				value       : val
			});
		});
		nodes.stream_exclusive_unmute.addEventListener('click', (event) => {
			ws_sendmulti(i, "stream_ctl", "mute", "yes");  // TODO
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "mute",
				value       : "no",
			});
		});
		nodes.stream_stop.addEventListener('click', (event) => {
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "play",
				value       : "no"
			});
		});
		nodes.stream_play.addEventListener('click', (event) => {
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "play",
				value       : "yes"
			});
		});
		nodes.stream_restart.addEventListener('click', (event) => {
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "play",
				value       : "restart"
			});
		});
		nodes.stream_ffwd.addEventListener('click', (event) => {
			fnordstream.ws_send({
				request     : "stream_ctl",
				stream_id   : i,
				ctl         : "seek",
				value       : "1"
			});
		});
		parent.appendChild(n);
		return nodes;
	}); // foreach stream
}

function assign_viewports() {
	// clear assigned viewports and streams first
	Object.values(fnordstreams).forEach(v => {
		v.viewports = [];
		v.stream_locations = [];
	});
	const viewports        = global.viewports;
	const stream_locations = global.stream_locations;
	// assign global.viewports and stream_location to fnordstream instances
	// TODO: map?
	viewports.forEach( (vp, idx) => {
		const stream_location = stream_locations[idx];
		const fnordstream = primary;     // TODO
		fnordstream.viewports.push(vp);
		fnordstream.stream_locations.push(stream_location);
	});
	draw_viewports();  // redraw viewports
}

// OK
function draw_viewports(fnordstream) {
	/* redraw all if not specified */
	if (!fnordstream) {
		Object.values(fnordstreams).forEach(v => v.peer ? draw_viewports(v) : null);
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

	viewports.forEach( (geo,i) => {
		//ctx.fillStyle='#aaaaaa';
		//ctx.fillRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.strokeStyle = lightmode ? "#000000" : "#ffffff";
		ctx.strokeRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);

		const font_h  = geo.h/16;
		ctx.fillStyle = lightmode ? "#000000" : "#ffffff";
		ctx.font      = font_h+'px sans-serif';
		ctx.fillText(i, 3.5+geo.x/8 + geo.w/16, (geo.y/8)+(geo.h/16)+(font_h/2)-2, geo.w/8);
	});
}

// OK
function draw_displays(fnordstream) {
	/* redraw all if not specified */
	if (!fnordstream) {
		Object.values(fnordstreams).forEach(v => v.peer ? draw_displays(v) : null);
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

// TODO: same IDs for multiple hosts??
// discard old displays table and rebuild
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
	primary.ws_send({
		request   : "suggest_viewports",
		n_streams : global.stream_locations.length,
		displays  : global.displays,
		discard   : true
	});
}

function displays_notification(fnordstream, msg) {
	v = msg.payload
	if ((!v) || (v.length < 1)) {
		const toast = new bootstrap.Toast(document.getElementById('displaydetect_failed'));
		toast.show();
		//displays = [{"name":"Default","use":true,"geo":{"x":0,"y":0,"w":window.screen.width,"h":window.screen.height}}];
		//set_displays();
		return;
	}
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
	assign_viewports();
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

// TODO: global playing status
function streams_playing(active) {
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

// TODO: global playing
function global_status(fnordstream, msg) {
	const status = msg.payload;

	if ((!status) || (status.length < 1))
		return;

	document.title = "fnordstream v"+status.version+" @"+window.location.hostname;

	streams_playing(status.playing);

	if (!status.playing)
		return;

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
		}
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

// TBD
function populate_cmds_table(cmds) {
	let template = document.getElementById('cmd-');
	let parent   = template.parentNode;
	parent.replaceChildren(template);

	const sorted = ["mpv", "yt-dlp", "streamlink", "xrandr"];

	for (let i=0;i<sorted.length;i++) {
		let n = template.cloneNode(true);
		let children = n.childNodes;
		n.id += i;
		n.hidden = false;

		let cmd_name = sorted[i];
		let cmd = cmds[cmd_name];
		if (!cmd) continue;

		const code     = parseInt(cmd.exit_code);
		const required = required_commands[cmd_name];

		n.classList.add( (code == 0) ? "table-success" : (required ? "table-danger" : "table-warning"));

		let required_node = replace_child(children,"cmd-required-",i);
		required_node.textContent = required ? "required" : "optional";

		let name_node = replace_child(children,"cmd-cmd-",i);
		name_node.innerHTML = "<b>" + mklink(cmd_name) + "</b>";

		let exitcode_node = replace_child(children,"cmd-exitcode-",i);
		if (code)
			exitcode_node.innerHTML = "<b>" + cmd.exit_code + "&#x2718;</b>";
		else
			exitcode_node.innerHTML = cmd.exit_code + "&#x2714;";

		let output_node = replace_child(children,"cmd-output-",i);
		output_node.textContent = cmd.error ? cmd.error : cmd.stdout;

		parent.appendChild(n);
	}
}

// TBD
let cmd_modal = undefined;

// TBD
function commands_probed(fnordstream, msg) {

	const results = msg.payload;
	if ((!results) || (results.length < 1))
		return;

	/* disable/enable use_streamlink switch */
	const use_streamlink = document.getElementById('use_streamlink');
	use_streamlink.disabled = results["streamlink"].exit_code != 0;
	const streamlink_note = document.getElementById('streamlink_note');
	streamlink_note.textContent = (results["streamlink"].exit_code != 0) ?
		"No working streamlink found." : "";

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
	const cmd_missing_alert = document.getElementById('cmd_missing_alert');
	const cmd_alert = (message, type) => {
	  const wrapper = document.createElement('div');
	  wrapper.innerHTML = [
		`<div class="alert alert-${type}" role="alert">`,
		`   <div>${message}</div>`,
		'   <div class="spinner-border spinner-border-sm" role="status" id="cmd_refresh_busy" hidden><span class="visually-hidden">Loading...</span></div>',
		'   <button type="button" class="btn btn-secondary btn-sm" id="commands_refresh"><i class="bi bi-arrow-clockwise"></i>&nbsp;Refresh</button>',
		'   <button type="button" class="btn btn-secondary btn-sm" id="commands_details" data-bs-toggle="modal" data-bs-target="#cmds_modal"><i class="bi bi-list-columns-reverse"></i>&nbsp;Details</button>',
		'</div>'
	  ].join('');
	  cmd_missing_alert.replaceChildren(wrapper);
	}
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
	cmd_alert(alert_msg, alert_type);

	/* button handlers for status note */
	const commands_refresh = document.getElementById('commands_refresh');
	commands_refresh.disabled = false;
	if (!commands_refresh.getAttribute('click_attached')) {
		commands_refresh.addEventListener('click', refresh_cmds);
		commands_refresh.setAttribute('click_attached', 'true');
	}

	const commands_refresh2 = document.getElementById('commands_refresh2');
	commands_refresh2.disabled = false;
	if (!commands_refresh2.getAttribute('click_attached')) {
		commands_refresh2.addEventListener('click', refresh_cmds);
		commands_refresh2.setAttribute('click_attached', 'true');
	}

	const cmd_refresh_busy = document.getElementById('cmd_refresh_busy');
	const cmd_refresh_busy2 = document.getElementById('cmd_refresh_busy2');
	cmd_refresh_busy2.hidden = true;

	const req_missing = document.getElementById('req_missing');
	req_missing.hidden = !(missing_required.length > 0);

	const close_btn1 = document.getElementById('cmd_modal_close1');
	const close_btn2 = document.getElementById('cmd_modal_close2');
	close_btn1.disabled = missing_required.length > 0;
	close_btn2.disabled = missing_required.length > 0;

	if (missing_required.length > 0) {
		cmd_modal = cmd_modal || new bootstrap.Modal(document.getElementById('cmds_modal'))
		cmd_modal.show();
	}

	function refresh_cmds() {
		if(!ws)
			return;
		ws.send(JSON.stringify({request : "probe_commands"}));
		commands_refresh.disabled = true;
		commands_refresh2.disabled = true;
		cmd_refresh_busy.hidden = false;
		cmd_refresh_busy2.hidden = false;
	}

	populate_cmds_table(results);
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

// TODO: remove
function replace_child(list,id,ext) {
	if ((!list) || (list.length < 1))
		return;

	let res = null;
	for (let i=0;i<list.length;i++) {
		let n = list[i];

		// replace for-tags as well
		if (n.attributes && n.attributes["for"] && (n.attributes["for"].value == id)) {
			n.attributes["for"].value += ext;
		}

		if (n.id == id) {
			n.id += ext;
			res = n;
		}

		let res2 = replace_child(n.childNodes, id, ext);
		if (res2)
			res = res2;
	}
	return res;
}

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
function create_displays(fnordstream) {
	const server  = fnordstream.peer.split(":")[0];
	const conn_id = fnordstream.conn_id;
	let template  = document.getElementById('display_table-');
	let parent    = template.parentNode;

	let n = template.cloneNode(true);
	let children = n.childNodes;
	n.id += conn_id;
	n.hidden = false;

	let nodes = adapt_nodes(children, conn_id);
	nodes.display_table = n;

	nodes.refresh_displays.addEventListener('click', (event) =>
		fnordstream.ws_send({request:"detect_displays"}));
	nodes.display_host.textContent = "@"+server+":";

	const info_tt = new bootstrap.Tooltip(nodes.display_info);

	fnordstream.display_nodes = nodes;

	parent.appendChild(n);
	nodes.refresh_displays.dispatchEvent(new Event("click"));
}

// OK
function ws_send(requests) {
	requests = Array.isArray(requests) ? requests : [requests];
	if(this == window) {
		Object.values(fnordstreams).forEach(v => v.ws_send(requests));
		return;
	}
	const buf = requests.reduce((res,v) => v ? res+JSON.stringify(v) : res, "");
	this.websock.send(buf);
}

function add_connection(dst) {
  dst += dst.search(":")<0 ? ":8090" : "";
  if (fnordstreams[dst]) return; // check for duplicate connections

  const websock = new WebSocket("ws://"+dst+"/ws");
  let fnordstream = {};

  websock.addEventListener('open', (event) => {
	fnordstream = {
		peer          : dst,
		websock       : websock,
		conn_id       : conn_id++,

		ws_send       : ws_send,

		displays      : [],
		viewports     : [],

		display_nodes : undefined,
		stream_nodes  : undefined,
	};
	fnordstreams[dst]   = fnordstream;
	fnordstream.primary = fnordstream.conn_id == 0;
	if(fnordstream.primary)
		primary = fnordstream;

	// create displays list for host
	create_displays(fnordstream);

	// send initial requests
	fnordstream.ws_send([
		{request : "global_status"},
		fnordstream.primary ? {request : "get_profiles"} : undefined,
		{request : "probe_commands"}
	]);

    document.getElementById('stream_urls').dispatchEvent(new Event("input"));
    console.log("websock opened",fnordstream.conn_id,dst);
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
	  // TODO: cleanup nodes
	  delete(fnordstreams[dst]);
	  global.update_displays();
	  console.log("websock closed", fnordstream.conn_id,dst);
  });
}

/*
function ws_sendmulti(exempt, request, ctl, value) {
	if(!ws) return;
	let msg = "";
	for (let i=0;i<stream_locations.length;i++) {
		if(i==exempt) continue;
		msg += JSON.stringify(
			{
				request     : request,
				stream_id   : i,
				ctl         : ctl,
				value       : value,
			});
	}
	ws.send(msg);
} */

document.addEventListener("DOMContentLoaded", function() {
  add_connection(window.location.host);
  register_handlers();
});
