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

function rfid_scan(data,textStatus) {

	var span = $('span#rfid');

	if ( span.size() == 0 ) // insert last in language bar on bottom
		span = $('ul#i18nMenu').append('<li><span id=rfid>RFID reader found<span>');

	if ( span.size() == 0 ) // or before login on top
		span = $('div#login').prepend('<span id=rfid>RFID reader found</span>');

	span = $('span#rfid');

	if ( data.tags ) {
		if ( data.tags.length === 1 ) {
			var t = data.tags[0];

			var url = document.location.toString();
			var circulation = url.substr(-14,14) == 'circulation.pl';
			var returns = url.substr(-10,10) == 'returns.pl';

			if ( t.content.length == 0 ) { // empty tag

				span.text( t.sid + ' empty' ).css('color', 'red' );

			} else if ( t.content.substr(0,3) == '130' ) { // books

				if ( circulation )
					 rfid_secure( t.content, t.sid, 'D7' );
				if ( returns )
					 rfid_secure( t.content, t.sid, 'DA' );

				var color = 'blue';
				if ( t.security.toUpperCase() == 'DA' ) color = 'red';
				if ( t.security.toUpperCase() == 'D7' ) color = 'green';
				span.text( t.content ).css('color', color);

				if ( ! rfid_submitted && ! barcode_on_screen( t.content ) ) {
					var last = sessionStorage.getItem('rfid_last_barcode');
					if ( t.content != last ) {
						sessionStorage.setItem('rfid_last_barcode', t.content);
						rfid_submitted = true;
						var i = $('input[name=barcode]:last');
						if ( i.val() != t.content ) 
							i.val( t.content )
								.closest('form').submit();
					}
				}

			} else {
				span.text( t.content ).css('color', 'blue' );

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
			span.text( error ).css( 'color', 'red' );
		}

	} else {
		span.text( 'no tags in range' ).css('color','gray');
	}

	if ( ! rfid_submitted ) {
		rfid_timeout = window.setTimeout( function() {
			$.getJSON("https://localhost:9000/scan?callback=?", rfid_scan);
		}, 1000 );
	}
}

$(document).ready( function() {
	rfid_submitted = false;
	rfid_timeout = null;
	$.getJSON("https://localhost:9000/scan?callback=?", rfid_scan);
});
