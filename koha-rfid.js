/*
 * RFID support for Koha
 *
 * Written by Dobrica Pavlinusic <dpavlin@rot13.org> under GPL v2 or later
 *
 * This provides example how to integrate JSONP interface from
 *
 * scripts/RFID-JSONP-server.pl
 *
 * to provide overlay for tags in range and emulate form fill for Koha Library System
 * which allows check-in and checkout-operations without touching html interface
 *
 * You will have to inject remote javascript in Koha intranetuserjs using:
 *
 * // inject JavaScript RFID support
 * $.getScript('https://localhost:9000/examples/koha-rfid.js');
 *
 */

var RFID_VERSION = '1.0';  // version number for tracking
var rfid_timeout = null;
var rfid_poll_pending = false;
var rfid_events = [];  // in-memory cache of recent events
var rfid_show_events = localStorage.getItem('rfid_show_events') == 'true';  // checkbox state
var rfid_page_id = null;  // unique page load identifier (set on ready)

// ---------------------------------------------------------------------------
// localStorage keys used:
//   rfid_events     — audit log array of { time, barcode, action, detail, page }
//   rfid_popup_pos  — saved popup position
// ---------------------------------------------------------------------------

// Read/write helpers for localStorage JSON objects
function rfid_storage_get(key, def) {
	var v = localStorage.getItem(key);
	return v ? JSON.parse(v) : (def || {});
}
function rfid_storage_set(key, obj) {
	localStorage.setItem(key, JSON.stringify(obj));
}

// ---------------------------------------------------------------------------
// Audit log — append-only event store with daily cleanup
//
// Each event:
//   time    — Date.now() ms
//   barcode — book barcode (130...)
//   action  — 'scan', 'submit-checkin', 'submit-checkout', 'submit-renew', 'afi-write'
//   detail  — AFI hex (DA/D7) or error string
//   page    — unique page load id (to identify events from the same page load)
// ---------------------------------------------------------------------------

function rfid_page() {
	if ( !rfid_page_id ) rfid_page_id = Date.now().toString(36) + Math.random().toString(36).slice(2,5);
	return rfid_page_id;
}

function rfid_event_push(barcode, action, detail) {
	// Skip consecutive duplicate scan events for the same barcode
	if ( action == 'scan' ) {
		var last = rfid_events.length ? rfid_events[rfid_events.length - 1] : null;
		if ( last && last.barcode == barcode && last.action == 'scan' && last.detail == detail ) {
			rfid_events[rfid_events.length - 1].time = Date.now(); // update timestamp in memory
			return;
		}
	}
	var events = rfid_storage_get('rfid_events', []);
	events.push({ time: Date.now(), barcode: barcode, action: action, detail: detail, page: rfid_page() });
	rfid_storage_set('rfid_events', events);
	rfid_events = events.slice(-20);
}

function rfid_event_cleanup() {
	var events = rfid_storage_get('rfid_events', []);
	var now = Date.now();
	var keep = events.filter(function(e) { return now - e.time < 86400000; });
	if ( keep.length != events.length ) {
		rfid_storage_set('rfid_events', keep);
	}
	rfid_events = keep.slice(-20);
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
// Derive state from audit log (replaces rfid_last_barcode / rfid_pending / koha_state)
// ---------------------------------------------------------------------------

// Return the most recent event for a barcode with a given action prefix, or null
function rfid_event_last(barcode, actionPrefix) {
	var events = rfid_storage_get('rfid_events', []);
	var best = null;
	for ( var i = events.length - 1; i >= 0; i-- ) {
		var e = events[i];
		if ( e.barcode == barcode && e.action.indexOf(actionPrefix) === 0 ) {
			if ( !best || e.time > best.time ) best = e;
		}
	}
	return best;
}

// Return true if this barcode was already submitted on the current page load
function rfid_already_submitted(barcode) {
	var events = rfid_storage_get('rfid_events', []);
	var page = rfid_page();
	for ( var i = events.length - 1; i >= 0; i-- ) {
		var e = events[i];
		if ( e.barcode == barcode && e.page == page ) {
			if ( e.action.indexOf('submit-') === 0 ) return true;
		}
	}
	return false;
}

// Return the AFI target for a pending write, or null if none needed
// A pending write exists when the most recent event for this barcode is
// a submit-* that should change AFI, and there is no afi-write after it.
function rfid_pending_target(barcode) {
	var lastSubmit = rfid_event_last(barcode, 'submit-');
	if ( !lastSubmit ) return null;
	var lastWrite = rfid_event_last(barcode, 'afi-write');
	if ( lastWrite && lastWrite.time >= lastSubmit.time ) return null; // write already done
	if ( lastSubmit.action == 'submit-checkin' ) return 'DA';
	if ( lastSubmit.action == 'submit-checkout' ) return 'D7';
	return null; // renew or other — no AFI change needed
}

// Return the Koha-verified AFI for a barcode (what Koha should have set)
function rfid_koha_target(barcode) {
	var lastSubmit = rfid_event_last(barcode, 'submit-');
	if ( !lastSubmit ) return null;
	if ( lastSubmit.action == 'submit-checkin' ) return 'DA';
	if ( lastSubmit.action == 'submit-checkout' ) return 'D7';
	return null;
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
	var url = 'https://localhost:9000/secure/' + sid + '/' + target;
	rfid_fetch(url, 15000).then(function(r) {
		if ( r.ok ) {
			rfid_event_push(barcode, 'afi-write', target);
			var body = $('#rfid-popup-body');
			body.text( barcode + ' (' + afi_label(target) + ')' ).css('color', afi_color(target));
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
	body.html(msg + (hint ? link : '')).css('color', 'orange');
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
	return rfid_fetch('https://localhost:9000/ping', 3000).then(function(r) {
		if ( r.ok ) return true;
		throw new Error('HTTP ' + r.status);
	}).catch(function(e) {
		throw e;
	});
}

function rfid_poll() {
	if ( rfid_poll_pending ) return;
	rfid_poll_pending = true;

	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();

	rfid_check_server().then(function() {
		var timeout = window.setTimeout(function() {
			rfid_poll_pending = false;
			rfid_show_error('RFID server not responding (reader timeout)', true);
		}, 5000);

		rfid_fetch('https://localhost:9000/scan/', 15000).then(function(r) {
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
	});
}

// ---------------------------------------------------------------------------
// rfid_scan — the main RFID scan handler
//
// Order of operations per page:
//
// RETURNS (checkin):
//   1. Submit to Koha once per state change (fill #barcode + submit)
//   2. Record submit-checkin event in audit log
//   3. Page reloads → on next scan, if pending AFI write is needed, perform it
//
// CIRCULATION (checkout):
//   1. Only if tag AFI == DA (checked in) → fill barcode + submit to Koha
//   2. Record submit-checkout event in audit log
//   3. Page reloads → on next scan, write D7 if pending
//
// RENEW:
//   1. Fill #ren_barcode + submit (tag must be D7 — on loan)
//   2. No AFI write needed (loan status unchanged)
//
// PENDING AFI RESOLUTION (runs before any new form submission):
//   If audit log shows a submit-* without a subsequent afi-write, and the
//   tag's current AFI matches the expected pre-submit AFI, perform the write now.
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
				var pendingTarget = rfid_pending_target(t.content);
				if ( pendingTarget && sec != pendingTarget ) {
					// The tag still has the old AFI — perform the write now
					body.text( t.content + ' (writing ' + pendingTarget + '...)' ).css('color', '#888');
					rfid_secure( t.content, t.sid, pendingTarget );
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}
				if ( pendingTarget && sec == pendingTarget ) {
					// The write already happened (maybe on a previous page load) — just record it
					rfid_event_push(t.content, 'afi-write', pendingTarget);
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 2: skip if this barcode was already submitted on this page
				// -----------------------------------------------------------
				if ( rfid_already_submitted(t.content) ) {
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
							i.closest('form').submit();
						}
					} else {
						body.text( t.content + ' (not on loan — cannot renew)' ).css('color', 'blue');
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 4: Checkin (returns.pl) — submit to Koha once per state change
				// -----------------------------------------------------------
				if ( returns ) {
					// Only submit if Koha state doesn't already match the tag AFI.
					var ks = rfid_koha_target(t.content);
					if ( ks != sec ) {
						var i = $('#barcode');
						if ( i.val() != t.content ) {
							i.val( t.content );
							rfid_event_push(t.content, 'submit-checkin', sec);
							i.closest('form').submit();
						}
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
				if ( !rfid_already_submitted(t.content) && ( url.indexOf('circulation.pl') < 0 || $('form[name=mainform]').size() == 0 ) ) {
					rfid_event_push(t.content, 'patron-scan', '');
					$('input[name=findborrower]').val( t.content )
						.parent().submit();
				}
			}

		} else {
			var error = data.tags.length + ' tags near reader: ';
			$.each( data.tags, function(i,tag) { error += tag.content + ' '; } );
			body.text( error ).css( 'color', 'red' );
		}

	} else {
		body.text( 'no tags in range' ).css('color','gray');
	}

	rfid_event_update();
	rfid_timeout = window.setTimeout( rfid_poll, 1000 );
}

$(document).ready( function() {
	rfid_event_cleanup();
	rfid_timeout = null;
	rfid_poll();
});
