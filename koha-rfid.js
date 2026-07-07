/*
 * RFID support for Koha
 *
 * Written by Dobrica Pavlinusic <dpavlin@rot13.org> under GPL v2 or later
 *
 * This provides example how to implement JSONP interface from
 *
 * scripts/RFID-JSONP-server.pl
 *
 * to provide overlay for tags in range and emulate form fill for Koha Library System
 * which allows check-in and checkout-operations without touching html interface
 *
 * This file is injected by the Koha plugin (Koha::Plugin::Rot13::RFID)
 * only on RFID-relevant pages (circulation, returns, renew, mainpage).
 *
 */

var RFID_VERSION = '2.0';  // version number for tracking
var rfid_base_url = 'https://localhost:9000'; // override for mock-server testing
var rfid_timeout = null;
var rfid_poll_pending = false;
var rfid_server_ok = false; // set after first successful ping
var rfid_no_reader = localStorage.getItem('rfid_no_reader') == 'true'; // user opted out
var rfid_events = [];  // in-memory event log for popup display only (not persisted)
var rfid_show_events = localStorage.getItem('rfid_show_events') == 'true';  // checkbox state

// Debug namespace — exposed on window for rodney inspection
window.rfidDebug = {};

// In-memory set of barcodes already submitted on this page load (prevents re-submit loops)
var rfid_submitted_this_page = {};

// Expose key variables for rodney inspection
window.rfidDebug.events = rfid_events;
window.rfidDebug.afiMap = function() { return rfid_storage_get('rfid_afi', {}); };
window.rfidDebug.localStorage = function() { return JSON.parse(JSON.stringify(localStorage)); };
window.rfidDebug.serverOk = rfid_server_ok;
window.rfidDebug.noReader = rfid_no_reader;
window.rfidDebug.submittedThisPage = rfid_submitted_this_page;

function rfid_storage_get(key, def) {
	var v = localStorage.getItem(key);
	return v ? JSON.parse(v) : (def || {});
}
function rfid_storage_set(key, obj) {
	localStorage.setItem(key, JSON.stringify(obj));
}

// ---------------------------------------------------------------------------
// AFI map — per-barcode state in localStorage
//
// Key: rfid_afi
// Value: { barcode: { sec: string, pending: string|null, time: number } }
//
//   sec     — last known AFI from tag (DA = checked in, D7 = on loan)
//   pending — target AFI to write after a submit (null = no pending write)
//   time    — timestamp of last update (ms)
//
// The AFI map replaces the event log for dedup and pending-write tracking.
// Events are kept in-memory only for popup display.
// ---------------------------------------------------------------------------

// Get the AFI entry for a barcode, or null if not found
function rfid_afi_get(barcode) {
	var map = rfid_storage_get('rfid_afi', {});
	return map[barcode] || null;
}

// Set/update the AFI entry for a barcode
function rfid_afi_set(barcode, sec, pending) {
	var map = rfid_storage_get('rfid_afi', {});
	map[barcode] = { sec: sec, pending: pending || null, time: Date.now() };
	rfid_storage_set('rfid_afi', map);
}

// Clear the pending flag for a barcode (write completed)
function rfid_afi_clear_pending(barcode) {
	var map = rfid_storage_get('rfid_afi', {});
	if (map[barcode]) {
		map[barcode].pending = null;
		map[barcode].time = Date.now();
		rfid_storage_set('rfid_afi', map);
	}
}

// Remove entries older than 1 hour
function rfid_afi_cleanup() {
	var map = rfid_storage_get('rfid_afi', {});
	var now = Date.now();
	var changed = false;
	for (var key in map) {
		if (now - map[key].time > 3600000) { // 1 hour retention
			delete map[key];
			changed = true;
		}
	}
	if (changed) rfid_storage_set('rfid_afi', map);
}

// ---------------------------------------------------------------------------
// In-memory event log (for popup display only, not persisted)
// ---------------------------------------------------------------------------

function rfid_event_push(barcode, action, detail) {
	// Skip consecutive duplicate scan events for the same barcode
	if ( action == 'scan' ) {
		var last = rfid_events.length ? rfid_events[rfid_events.length - 1] : null;
		if ( last && last.barcode == barcode && last.action == 'scan' && last.detail == detail ) {
			rfid_events[rfid_events.length - 1].time = Date.now(); // update timestamp in memory
			return;
		}
	}
	rfid_events.push({ time: Date.now(), barcode: barcode, action: action, detail: detail });
	rfid_events = rfid_events.slice(-20);
}

function rfid_event_format(e) {
	var t = new Date(e.time);
	var h = t.getHours().toString().padStart(2, '0');
	var m = t.getMinutes().toString().padStart(2, '0');
	var s = t.getSeconds().toString().padStart(2, '0');
	return h + ':' + m + ':' + s + ' ' + e.barcode + ' ' + e.action + (e.detail ? ': ' + e.detail : '');
}

function rfid_event_update() {
	rfid_show_events = $('#rfid-events-check').prop('checked');
	localStorage.setItem('rfid_show_events', rfid_show_events ? 'true' : 'false');
	var body = $('#rfid-popup-body');
	var log = $('#rfid-events-log');
	if ( rfid_show_events ) {
		if ( log.length == 0 ) {
			var html = '<div id="rfid-events-log" style="font-size:11px; line-height:1.4; max-height:200px; overflow-y:auto; border-top:1px solid #555; padding-top:4px; margin-top:4px">';
			if ( rfid_events.length == 0 ) {
				html += '<span style="color:#888">(no recent events)</span>';
			} else {
				for ( var i = rfid_events.length - 1; i >= 0; i-- ) {
					html += '<div>' + rfid_event_format(rfid_events[i]) + '</div>';
				}
			}
			html += '</div>';
			var el = body[0];
			if ( el ) el.insertAdjacentHTML('afterend', html);
		} else {
			var html = '';
			if ( rfid_events.length == 0 ) {
				html = '<span style="color:#888">(no recent events)</span>';
			} else {
				for ( var i = rfid_events.length - 1; i >= 0; i-- ) {
					html += '<div>' + rfid_event_format(rfid_events[i]) + '</div>';
				}
			}
			log.html(html);
		}
	} else {
		log.remove();
	}
}

// ---------------------------------------------------------------------------
// AFI values from RFID server (always uppercase hex):
//   DA = secured (checked in), door ignores
//   D7 = unsecured (checked out), door beeps
// For checkout we need DA (item is in library), for checkin we need D7 (item was on loan)
function afi_label(sec) {
	var s = (sec || '').toUpperCase();
	return s == 'DA' ? 'checked in' : s == 'D7' ? 'on loan' : 'unknown';
}
function afi_color(sec) {
	var s = (sec || '').toUpperCase();
	return s == 'DA' ? 'red' : s == 'D7' ? 'green' : 'blue';
}

// ---------------------------------------------------------------------------
// RFID secure — write AFI to tag via RFID server
// ---------------------------------------------------------------------------

function rfid_secure(barcode, sid, target) {
	var url = rfid_base_url + '/secure.js?' + sid + '=' + target + '&callback=jsonp';
	rfid_fetch(url, 15000).then(function(r) {
		if ( r.status == 200 ) {
			rfid_event_push(barcode, 'afi-write', target);
			rfid_afi_clear_pending(barcode);
		} else {
			rfid_event_push(barcode, 'afi-write', 'error ' + r.status);
		}
	}).catch(function(e) {
		rfid_event_push(barcode, 'afi-write', 'error: ' + e.message);
	});
}

// ---------------------------------------------------------------------------
// Popup
// ---------------------------------------------------------------------------

function rfid_show_popup_body() {
	var body = $('#rfid-popup-body');
	var text = body.data('last-text') || '—';
	var color = body.data('last-color') || 'gray';
	body.text(text).css('color', color);
	rfid_event_update();
}

function rfid_create_popup() {
	var saved = localStorage.getItem('rfid_popup_pos');
	var pos = saved ? JSON.parse(saved) : { top: 10, right: 10 };
	var checked = rfid_show_events ? ' checked' : '';
	var html =
		'<div id="rfid-popup" style="' +
			'position:fixed; z-index:9999;' +
			'top:' + pos.top + 'px; right:' + pos.right + 'px;' +
			'background:#333; color:#fff; padding:10px 14px;' +
			'border-radius:6px; font-size:13px; font-family:monospace;' +
			'cursor:move; box-shadow:2px 2px 8px rgba(0,0,0,0.4);' +
			'min-width:200px;' +
		'">' +
			'<div id="rfid-popup-header" style="font-weight:bold; margin-bottom:4px;">RFID reader v' + RFID_VERSION + '</div>' +
			'<div id="rfid-popup-body">—</div>' +
			'<div style="font-size:10px; margin-top:4px; opacity:0.6">' +
				'<label style="color:#aaa; cursor:pointer">' +
					'<input type="checkbox" id="rfid-events-check"' + checked + '> events' +
				'</label>' +
			'</div>' +
		'</div>';

	$('body').append(html);

	var popup = $('#rfid-popup');
	var header = $('#rfid-popup-header');
	var drag = false, offsetX, offsetY;

	header.on('mousedown', function(e) {
		drag = true;
		var p = popup.position();
		offsetX = e.clientX - p.left;
		offsetY = e.clientY - p.top;
	});

	$(document).on('mouseup', function() {
		if (drag) {
			drag = false;
			var p = popup.position();
			var w = $(window).width();
			var h = $(window).height();
			var pw = popup.outerWidth();
			localStorage.setItem('rfid_popup_pos', JSON.stringify({
				top: Math.min(p.top, h - 60),
				right: Math.max(w - p.left - pw, 0)
			}));
		}
	});

	$(document).on('mousemove', function(e) {
		if (drag) {
			popup.css({ top: e.clientY - offsetY, left: e.clientX - offsetX });
		}
	});

	$('#rfid-events-check').on('change', function(e) {
		rfid_event_update();
	});

	return $('#rfid-popup-body');
}

function rfid_show_error(msg, hint) {
	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();
	var link = ' — <a href="https://localhost:9000" target="_blank" style="color:orange;text-decoration:underline">open https://localhost:9000</a> in a new tab and accept self-signed certificate';
	var buttons = '';
	if (hint) {
		buttons =
			'<div style="margin-top:6px">' +
				'<button id="rfid-retry-btn" style="cursor:pointer;background:#555;color:#fff;border:none;padding:3px 8px;border-radius:3px;margin-right:4px">retry</button>' +
				'<button id="rfid-no-reader-btn" style="cursor:pointer;background:#555;color:#fff;border:none;padding:3px 8px;border-radius:3px">no reader</button>' +
			'</div>';
	}
	body.html(msg + (hint ? link : '') + buttons).css('color', 'orange');
	if (hint) {
		$('#rfid-retry-btn').on('click', function() {
			rfid_no_reader = false;
			localStorage.removeItem('rfid_no_reader');
			rfid_poll();
		});
		$('#rfid-no-reader-btn').on('click', function() {
			rfid_no_reader = true;
			localStorage.setItem('rfid_no_reader', 'true');
			body.html('RFID disabled — <a href="#" id="rfid-enable-btn" style="color:orange;text-decoration:underline">enable</a>').css('color', '#888');
			$('#rfid-enable-btn').on('click', function() {
				rfid_no_reader = false;
				localStorage.removeItem('rfid_no_reader');
				rfid_poll();
			});
		});
	}
	rfid_event_update();
}

function rfid_fetch(url, timeout_ms) {
	var controller = new AbortController();
	var timer = setTimeout(function() { controller.abort(); }, timeout_ms);
	return fetch(url, { signal: controller.signal }).then(function(r) {
		clearTimeout(timer);
		return r;
	}).catch(function(e) {
		clearTimeout(timer);
		throw e;
	});
}

function rfid_check_server() {
	return rfid_fetch(rfid_base_url + '/ping', 3000).then(function(r) {
		if ( r.ok ) {
			rfid_server_ok = true;
			return true;
		}
		throw new Error('HTTP ' + r.status);
	}).catch(function(e) {
		throw e;
	});
}

function rfid_poll() {
	if ( rfid_poll_pending ) return;
	if ( rfid_no_reader ) return;
	rfid_poll_pending = true;

	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();

	// Ping once — after success skip it on subsequent polls
	var ping = rfid_server_ok ? Promise.resolve(true) : rfid_check_server();

	ping.then(function() {
		var timeout = window.setTimeout(function() {
			rfid_poll_pending = false;
			rfid_show_error('RFID server not responding (reader timeout)', true);
		}, 20000);

		rfid_fetch(rfid_base_url + '/scan/', 15000).then(function(r) {
			window.clearTimeout(timeout);
			if ( r.ok ) return r.json();
			throw new Error('HTTP ' + r.status);
		}).then(function(data) {
			rfid_poll_pending = false;
			rfid_scan(data);
		}).catch(function(e) {
			window.clearTimeout(timeout);
			rfid_poll_pending = false;
			rfid_show_error('RFID scan error: ' + e.message, true);
			rfid_timeout = window.setTimeout( rfid_poll, 5000 );
		});
	}).catch(function(e) {
		rfid_poll_pending = false;
		var msg = 'RFID server not reachable';
		if ( e.name == 'TypeError' || e.message.indexOf('Failed to fetch') >= 0 ) {
			msg += ' (connection refused or TLS error)';
		} else if ( e.message.indexOf('abort') >= 0 || e.message.indexOf('timeout') >= 0 ) {
			msg += ' (timeout)';
		} else if ( e.message.indexOf('HTTP') >= 0 ) {
			msg += ' (' + e.message + ')';
		}
		rfid_show_error(msg, true);
		rfid_timeout = window.setTimeout( rfid_poll, 5000 );
	});
}

// ---------------------------------------------------------------------------
// rfid_scan — the main RFID scan handler
//
// Order of operations (AFI map is the source of truth):
//
// 1. PENDING AFI RESOLUTION:
//    If the AFI map has a pending target for this barcode and the tag AFI
//    doesn't match, write the pending AFI now and return.
//
// 2. STATE CHECK:
//    If the tag AFI matches the stored AFI in the map, no state change
//    occurred — skip submission (book already processed).
//
// 3. NEW SUBMISSION:
//    Otherwise, submit the form and update the AFI map.
//    If the action changes the AFI (checkin/checkout), set pending target.
//
// ACROSS PAGE RELOADS:
//    The AFI map persists in localStorage. After page reload, the scan loop
//    will detect pending writes or unchanged AFIs and act accordingly.
// ---------------------------------------------------------------------------

function rfid_scan(data) {

	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();

	var checkin_active = $('#checkin_search').attr('aria-hidden') == 'false';
	var checkout_active = $('#checkout_search').attr('aria-hidden') == 'false';

	if ( data.tags && data.tags.length > 0 ) {
		if ( data.tags.length === 1 ) {
			var t = data.tags[0];

			var url = document.location.toString();
			var circulation = url.indexOf('circulation.pl') >= 0;
			var returns = url.indexOf('returns.pl') >= 0;
			var renew = url.indexOf('renew.pl') >= 0;

			if ( t.content.length == 0 ) { // empty tag

				body.text( t.sid + ' empty' ).css('color', 'red' );

			} else if ( t.content.substr(0,3) == '130' ) { // books

				var sec = (t.security || '').toUpperCase();
				var label = afi_label(sec);
				var color = afi_color(sec);
				body.text( t.content + ' (' + label + ')' ).css('color', color);

				rfid_event_push(t.content, 'scan', sec);

				// -----------------------------------------------------------
				// Step 1: resolve any pending AFI write from a previous page load
				// -----------------------------------------------------------
				var entry = rfid_afi_get(t.content);
				if ( entry && entry.pending ) {
					if ( entry.pending != sec ) {
						// Tag still has the old AFI — perform the write now
						body.text( t.content + ' (writing ' + entry.pending + '...)' ).css('color', '#888');
						rfid_secure( t.content, t.sid, entry.pending );
						rfid_timeout = window.setTimeout( rfid_poll, 1000 );
						return;
					} else {
						// The write already happened — clear pending and update stored sec
						rfid_afi_set(t.content, sec, null);
						rfid_timeout = window.setTimeout( rfid_poll, 1000 );
						return;
					}
				}

				// -----------------------------------------------------------
				// Step 2: skip if AFI hasn't changed since last submission
				// (returns page always re-submits to update date-last-seen)
				// -----------------------------------------------------------
				if ( entry && entry.sec == sec && !returns ) {
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 3: Renew page — #ren_barcode form, no AFI write
				// -----------------------------------------------------------
				if ( renew ) {
					if ( sec == 'D7' ) {
						var i = $('#ren_barcode');
						if ( i.val() != t.content ) {
							i.val( t.content );
							rfid_event_push(t.content, 'submit-renew', sec);
							rfid_afi_set(t.content, sec, null); // no AFI change needed
							i.closest('form').submit();
						}
					} else {
						body.text( t.content + ' (not on loan — cannot renew)' ).css('color', 'blue');
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 4: Checkin (returns.pl)
				// Submit only if tag AFI is D7 (on loan) — DA means already checked in
				// -----------------------------------------------------------
				if ( returns ) {
					if ( sec == 'D7' ) {
						var i = $('#barcode');
						if ( i.val() != t.content ) {
							i.val( t.content );
							rfid_event_push(t.content, 'submit-checkin', sec);
							rfid_afi_set(t.content, sec, 'DA'); // pending AFI write to DA
							i.closest('form').submit();
						}
					} else {
						body.text( t.content + ' (already checked in)' ).css('color', 'blue');
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 5: Circulation (checkout) — only if tag says checked in
				// -----------------------------------------------------------
				if ( circulation ) {
					if ( sec == 'DA' ) {
						var is_checkout = checkout_active || (!checkin_active && circulation);
						var is_checkin = checkin_active || returns;
						if ( is_checkout ) {
							var i = $('input[name=barcode]:last');
							if ( i.val() != t.content ) {
								i.val( t.content );
								rfid_event_push(t.content, 'submit-checkout', sec);
								rfid_afi_set(t.content, sec, 'D7'); // pending AFI write to D7
								i.closest('form').submit();
							}
						}
					} else {
						body.text( t.content + ' (not checked in — cannot checkout)' ).css('color', 'blue');
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Fallback: not on a known Koha page — show info
				// -----------------------------------------------------------
				body.text( t.content + ' (' + label + ')' ).css('color', color);

			} else {
				// Non-book barcode (patron card)
				body.text( t.content ).css('color', 'blue' );
				var patronEntry = rfid_afi_get(t.content);
				if ( !patronEntry || patronEntry.sec != 'patron' ) {
					rfid_event_push(t.content, 'patron-scan', '');
					rfid_afi_set(t.content, 'patron', null);
					$('input[name=findborrower]').val( t.content )
						.parent().submit();
				}
			}

		} else {
			// Multiple tags — iterate and process the first unprocessed book
			for ( var ti = 0; ti < data.tags.length; ti++ ) {
				var t2 = data.tags[ti];
				if ( t2.content.length == 0 ) continue;
				if ( t2.content.substr(0,3) == '130' ) {
					var entry2 = rfid_afi_get(t2.content);
					var sec2 = (t2.security || '').toUpperCase();
					if ( !entry2 || entry2.sec != sec2 ) {
						// Process this single book; remaining tags will be picked up on next poll
						data.tags = [ t2 ];
						rfid_scan(data);
						return;
					}
				}
			}
			// All tags already processed — just show count
			var error = data.tags.length + ' tags (all processed)';
			$.each( data.tags, function(i,tag) { error += ' ' + tag.content; } );
			body.text( error ).css( 'color', '#888' );
		}

	} else {
		body.text( 'no tags in range' ).css('color','gray');
	}

	rfid_event_update();
	rfid_timeout = window.setTimeout( rfid_poll, 1000 );
}

$(document).ready( function() {
	rfid_afi_cleanup();
	// Remove old event log key (migrated to AFI map in v2.0)
	localStorage.removeItem('rfid_events');
	rfid_timeout = null;
	rfid_poll();
});
