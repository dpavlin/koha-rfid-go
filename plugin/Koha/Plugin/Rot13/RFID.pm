package Koha::Plugin::Rot13::RFID;

use Modern::Perl;

## Required for all plugins
use base qw(Koha::Plugins::Base);

use C4::Context;
use File::Slurp qw(read_file);
use utf8;

## Plugin version
our $VERSION = "1.0.0";

## Metadata
our $metadata = {
    name            => 'RFID Integration',
    author          => 'Dobrica Pavlinusic',
    date_authored   => '2026-07-06',
    date_updated    => '2026-07-06',
    minimum_version => undef,
    maximum_version => undef,
    version         => $VERSION,
    description     => 'Inject koha-rfid.js only on pages that need RFID support (circulation, returns, renew, mainpage)',
    namespace       => 'rfid',
};

sub new {
    my ( $class, $args ) = @_;

    $args->{'metadata'} = $metadata;
    $args->{'metadata'}->{'class'} = $class;

    my $self = $class->SUPER::new($args);

    return $self;
}

## Inject JavaScript only on RFID-relevant pages
sub intranet_js {
    my ( $self, $args ) = @_;

    # Check the current request URI to determine if we should inject RFID JS
    # Uses ENV because $self->{'cgi'} is not set when called via template plugin
    my $uri = $ENV{SCRIPT_NAME} || $ENV{REQUEST_URI} || '';

    # Pages that need RFID support
    # SYNC: keep in sync with page detection in koha-rfid.js (rfid_scan)
    my @rfid_pages = (
        'circulation.pl',      # also matches circulation-home.pl (substring)
        'circulation-home.pl', # explicit entry for sync clarity
        'returns.pl',
        'renew.pl',
        'mainpage.pl',
    );

    my $should_inject = 0;
    for my $page (@rfid_pages) {
        if ( index($uri, $page) >= 0 ) {
            $should_inject = 1;
            last;
        }
    }

    return '' unless $should_inject;

    my $dir = C4::Context->config('pluginsdir');
    my $plugin_fulldir = $dir . '/Koha/Plugin/Rot13/RFID/';
    my $js = read_file( $plugin_fulldir . 'koha-rfid.js' );

    utf8::decode($js);
    return "<script>$js</script>";
}

## Clean up on uninstall
sub uninstall {
    my ( $self, $args ) = @_;
}

## Upgrade handler
sub upgrade {
    my ( $self, $args ) = @_;

    return 1;
}

1;
