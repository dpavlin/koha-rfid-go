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

var rfid_timeout = null;
var rfid_poll_pending = false;

// ---------------------------------------------------------------------------
// localStorage keys used:
//   rfid_pending    — pending AFI write after Koha form submission (survives page reload)
//   koha_state      — cached Koha-verified state for each barcode
//   rfid_last_barcode — last submitted barcode (prevents double-submit on same page load)
// ---------------------------------------------------------------------------

// Read/write helpers for localStorage JSON objects
function rfid_storage_get(key, def) {
	var v = localStorage.getItem(key);
	return v ? JSON.parse(v) : (def || {});
}
function rfid_storage_set(key, obj) {
	localStorage.setItem(key, JSON.stringify(obj));
}

function barcode_on_screen(barcode) {
	var found = 0;
	$('table tr td a:contains(130)').each( function(i,o) {
		var possible = $(o).text();
		if ( possible == barcode ) found++;
	})
	return found;
}

function rfid_secure(barcode, sid, val) {
	if ( ! barcode_on_screen(barcode) ) return;

	var url = 'https://localhost:9000/secure.js?' + sid + '=' + val;
	var controller = new AbortController();
	var timer = setTimeout(function() { controller.abort(); }, 5000);
	fetch(url, { signal: controller.signal }).then(function(r) {
		clearTimeout(timer);
		if ( ! r.ok ) throw new Error('HTTP ' + r.status);
	}).catch(function(e) {
		clearTimeout(timer);
		var body = $('#rfid-popup-body');
		if ( body.length ) body.html('RFID write error: ' + e.message).css('color', 'orange');
	});
}

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
// Pending AFI writes — survive page reload via localStorage
//
// Before submitting a Koha form we store:
//   rfid_pending[barcode] = { target: 'DA'|'D7', current: 'DA'|'D7', time: ms }
//
// After page reload, if the same tag is still on reader with the same
// current AFI (meaning the write hasn't happened yet), we perform it.
// ---------------------------------------------------------------------------

function rfid_pending_set(barcode, target, current) {
	var p = rfid_storage_get('rfid_pending', {});
	p[barcode] = { target: target, current: current, time: Date.now() };
	rfid_storage_set('rfid_pending', p);
}

function rfid_pending_get(barcode) {
	var p = rfid_storage_get('rfid_pending', {});
	return p[barcode] || null;
}

function rfid_pending_clear(barcode) {
	var p = rfid_storage_get('rfid_pending', {});
	delete p[barcode];
	rfid_storage_set('rfid_pending', p);
}

// ---------------------------------------------------------------------------
// Koha-verified state cache — remembers what Koha says about each barcode
//
// After successful Koha processing we store:
//   koha_state[barcode] = 'DA'|'D7'  (what Koha believes the loan status is)
//
// On next scan we can compare tag AFI vs koha_state to detect mismatches.
// ---------------------------------------------------------------------------

function rfid_koha_state_get(barcode) {
	var s = rfid_storage_get('koha_state', {});
	return s[barcode] || null;
}

function rfid_koha_state_set(barcode, state) {
	var s = rfid_storage_get('koha_state', {});
	s[barcode] = state;
	rfid_storage_set('koha_state', s);
}

// ---------------------------------------------------------------------------
// Popup
// ---------------------------------------------------------------------------

function rfid_create_popup() {
	var saved = localStorage.getItem('rfid_popup_pos');
	var pos = saved ? JSON.parse(saved) : { top: 10, right: 10 };
	var html =
		'<div id="rfid-popup" style="' +
			'position:fixed; z-index:9999;' +
			'top:' + pos.top + 'px; right:' + pos.right + 'px;' +
			'background:#333; color:#fff; padding:10px 14px;' +
			'border-radius:6px; font-size:13px; font-family:monospace;' +
			'cursor:move; box-shadow:2px 2px 8px rgba(0,0,0,0.4);' +
			'min-width:200px;' +
		'">' +
			'<div id="rfid-popup-header" style="font-weight:bold; margin-bottom:4px;">RFID reader</div>' +
			'<div id="rfid-popup-body">—</div>' +
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

	return $('#rfid-popup-body');
}

function rfid_show_error(msg, hint) {
	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();
	var link = ' — <a href="https://localhost:9000" target="_blank" style="color:orange;text-decoration:underline">open https://localhost:9000</a> in a new tab and accept self-signed certificate';
	body.html(msg + (hint ? link : '')).css('color', 'orange');
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

		rfid_fetch('https://localhost:9000/scan/', 5000).then(function(r) {
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
//   1. Always fill #ret_barcode + submit to Koha (regardless of current AFI)
//   2. Set pending_afi[barcode] = DA in localStorage BEFORE submit
//   3. Page reloads → on next scan, if tag still has old AFI, write DA
//   4. Update koha_state[barcode] = DA
//
// CIRCULATION (checkout):
//   1. Only if tag AFI == DA (checked in) → fill barcode + submit to Koha
//   2. Set pending_afi[barcode] = D7 in localStorage BEFORE submit
//   3. Page reloads → on next scan, if tag still has DA, write D7
//   4. Update koha_state[barcode] = D7
//
// RENEW:
//   1. Fill #barcode + submit (tag must be D7 — on loan)
//   2. No AFI write needed (loan status unchanged)
//
// PENDING AFI RESOLUTION (runs before any new form submission):
//   If rfid_pending[barcode] exists and tag AFI matches the stored current AFI,
//   it means the page reloaded after Koha processed the form but the AFI write
//   hasn't happened yet. Perform it now.
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

				// -----------------------------------------------------------
				// Step 1: resolve any pending AFI write from a previous page load
				// -----------------------------------------------------------
				var pending = rfid_pending_get(t.content);
				if ( pending && pending.current == sec ) {
					// Page reloaded after Koha processed this barcode,
					// but the AFI write is still pending. Do it now.
					body.text( t.content + ' (writing ' + pending.target + '...)' ).css('color', '#888');
					rfid_secure( t.content, t.sid, pending.target );
					rfid_pending_clear(t.content);
					rfid_koha_state_set(t.content, pending.target);
					body.text( t.content + ' (' + afi_label(pending.target) + ')' ).css('color', afi_color(pending.target));
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}
				// If pending exists but current AFI already matches target,
				// the write happened on a previous page load — just clear state.
				if ( pending && pending.target == sec ) {
					rfid_pending_clear(t.content);
					rfid_koha_state_set(t.content, sec);
				}

				// -----------------------------------------------------------
				// Step 2: skip if this barcode was already submitted on this page
				// -----------------------------------------------------------
				var last = sessionStorage.getItem('rfid_last_barcode');
				if ( t.content == last ) {
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 3: Renew page — simple #barcode form, no AFI write
				// -----------------------------------------------------------
				if ( renew ) {
					if ( sec == 'D7' ) {
						sessionStorage.setItem('rfid_last_barcode', t.content);
						var i = $('#barcode');
						if ( i.val() != t.content ) {
							i.val( t.content );
							i.closest('form').submit();
						}
					} else {
						body.text( t.content + ' (not on loan — cannot renew)' ).css('color', 'blue');
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 4: Checkin (returns.pl) — always submit to Koha
				// -----------------------------------------------------------
				if ( returns ) {
					// Set pending AFI write BEFORE form submission.
					// After page reload, the pending resolution above will write DA.
					rfid_pending_set(t.content, 'DA', sec);
					sessionStorage.setItem('rfid_last_barcode', t.content);
					var i = $('#ret_barcode');
					if ( i.val() != t.content ) {
						i.val( t.content );
						i.closest('form').submit();
					}
					rfid_timeout = window.setTimeout( rfid_poll, 1000 );
					return;
				}

				// -----------------------------------------------------------
				// Step 5: Circulation (checkout) — only if tag says checked in
				// -----------------------------------------------------------
				if ( circulation ) {
					if ( sec == 'DA' ) {
						// Set pending AFI write BEFORE form submission.
						// After page reload, pending resolution will write D7.
						rfid_pending_set(t.content, 'D7', sec);
						sessionStorage.setItem('rfid_last_barcode', t.content);
						var is_checkout = checkout_active || (!checkin_active && circulation);
						var is_checkin = checkin_active || returns;
						if ( is_checkout ) {
							var i = $('input[name=barcode]:last');
							if ( i.val() != t.content ) {
								i.val( t.content );
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
				var last = sessionStorage.getItem('rfid_last_barcode');
				if ( t.content != last && ( url.indexOf('circulation.pl') < 0 || $('form[name=mainform]').size() == 0 ) ) {
					sessionStorage.setItem('rfid_last_barcode', t.content);
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
		sessionStorage.removeItem('rfid_last_barcode');
	}

	rfid_timeout = window.setTimeout( rfid_poll, 1000 );
}

$(document).ready( function() {
	rfid_timeout = null;
	rfid_poll();
});
