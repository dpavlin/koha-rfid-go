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

var rfid_submitted = false;
var rfid_timeout = null;
var rfid_poll_pending = false;

function barcode_on_screen(barcode) {
	var found = 0;
	$('table tr td a:contains(130)').each( function(i,o) {
		var possible = $(o).text();
		if ( possible == barcode ) found++;
	})
	return found;
}

function rfid_secure(barcode,sid,val) {
	if ( barcode_on_screen(barcode) ) 
		$.getJSON( 'https://localhost:9000/secure.js?' + sid + '=' + val + ';callback=?' )
}

// AFI values from RFID server (always uppercase hex):
//   DA = secured (checked in), door ignores
//   D7 = unsecured (checked out), door beeps
// For checkout we need DA (item is in library), for checkin we need D7 (item was on loan)
function afi_valid_for(security, page) {
	var s = security.toUpperCase();
	if (page == 'circulation') return s == 'DA';
	if (page == 'returns') return s == 'D7';
	return false;
}

// Create floating RFID status popup (called once at page load)
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

	// Make it draggable
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

function rfid_poll() {
	if ( rfid_poll_pending ) return;
	rfid_poll_pending = true;

	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();

	// timeout: if no response in 5 seconds, show connection error
	var poll_timeout = window.setTimeout(function() {
		rfid_poll_pending = false;
		body.html(
			'RFID server not reachable (TLS error?) — ' +
			'<a href="https://localhost:9000" target="_blank" style="color:orange;text-decoration:underline">' +
			'open https://localhost:9000</a> in a new tab and accept self-signed certificate'
		).css('color', 'orange');
	}, 5000);

	$.getJSON("https://localhost:9000/scan?callback=?", function(data, textStatus) {
		window.clearTimeout(poll_timeout);
		rfid_poll_pending = false;
		rfid_scan(data, textStatus);
	}).fail(function(jqXHR, textStatus, error) {
		window.clearTimeout(poll_timeout);
		rfid_poll_pending = false;
		body.html(
			'RFID server error: ' + textStatus + ' — ' +
			'<a href="https://localhost:9000" target="_blank" style="color:orange;text-decoration:underline">' +
			'open https://localhost:9000</a> in a new tab and accept self-signed certificate'
		).css('color', 'orange');
	});
}

function rfid_scan(data,textStatus) {

	var body = $('#rfid-popup-body');
	if ( body.length == 0 ) body = rfid_create_popup();

	// detect active tab: checkin tab has aria-hidden="false", checkout tab has aria-hidden="true"
	var checkin_active = $('#checkin_search').attr('aria-hidden') == 'false';
	var checkout_active = $('#checkout_search').attr('aria-hidden') == 'false';

	if ( data.tags ) {
		if ( data.tags.length === 1 ) {
			var t = data.tags[0];

			var url = document.location.toString();
			var circulation = url.substr(-14,14) == 'circulation.pl';
			var returns = url.substr(-10,10) == 'returns.pl';

			if ( t.content.length == 0 ) { // empty tag

				body.text( t.sid + ' empty' ).css('color', 'red' );

			} else if ( t.content.substr(0,3) == '130' ) { // books

				var sec = (t.security || '').toUpperCase();

				if ( circulation )
					 rfid_secure( t.content, t.sid, 'D7' );
				if ( returns )
					 rfid_secure( t.content, t.sid, 'DA' );

				var label = sec == 'DA' ? 'checked in' : sec == 'D7' ? 'on loan' : 'unknown';
				var color = sec == 'DA' ? 'red' : sec == 'D7' ? 'green' : 'blue';
				body.text( t.content + ' (' + label + ')' ).css('color', color);

				// determine which form to fill based on active tab, fall back to URL
				var is_checkout = checkout_active || (!checkin_active && circulation);
				var is_checkin = checkin_active || returns;

				if ( ! rfid_submitted && ! barcode_on_screen( t.content ) ) {

					if ( is_checkin && afi_valid_for(sec, 'returns') ) {
						// checkin form: use #ret_barcode
						var last = sessionStorage.getItem('rfid_last_barcode');
						if ( t.content != last ) {
							sessionStorage.setItem('rfid_last_barcode', t.content);
							rfid_submitted = true;
							var i = $('#ret_barcode');
							if ( i.val() != t.content ) {
								i.val( t.content );
								i.closest('form').submit();
							}
						}
					} else if ( is_checkout && afi_valid_for(sec, 'circulation') ) {
						// checkout form: use input[name=barcode]:last
						var last = sessionStorage.getItem('rfid_last_barcode');
						if ( t.content != last ) {
							sessionStorage.setItem('rfid_last_barcode', t.content);
							rfid_submitted = true;
							var i = $('input[name=barcode]:last');
							if ( i.val() != t.content ) {
								i.val( t.content );
								i.closest('form').submit();
							}
						}
					}
				}

			} else {
				body.text( t.content ).css('color', 'blue' );

				if ( ! rfid_submitted && ( url.substr(-14,14) != 'circulation.pl' || $('form[name=mainform]').size() == 0 ) ) {
					var last = sessionStorage.getItem('rfid_last_barcode');
					if ( t.content != last ) {
						sessionStorage.setItem('rfid_last_barcode', t.content);
						rfid_submitted = true;
						$('input[name=findborrower]').val( t.content )
							.parent().submit();
					}
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

	if ( ! rfid_submitted ) {
		rfid_timeout = window.setTimeout( rfid_poll, 1000 );
	}
}

$(document).ready( function() {
	rfid_submitted = false;
	rfid_timeout = null;
	rfid_poll();
});
