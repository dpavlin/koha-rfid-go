#!/usr/bin/env perl
use strict;
use warnings;
use Cwd qw(getcwd);

# Build KPZ plugin package — uses symlink to koha-rfid.js from repo root

my $repo_root  = getcwd() . '/..';
my $plugin_dir = './Koha/Plugin/Rot13/RFID';
my $version    = $ENV{VERSION} || '1.0.0';
my $kpz_file   = "koha-plugin-rfid-v$version.kpz";

# Clean previous build
unlink $kpz_file if -f $kpz_file;

# Create KPZ file (zip archive) — zip follows symlinks and stores target content
my @files = (
    'Koha/Plugin/Rot13/RFID.pm',
    'Koha/Plugin/Rot13/RFID/koha-rfid.js',
);

my $zip_cmd = "zip -r $kpz_file " . join(' ', @files);
print "Creating $kpz_file...\n";
system($zip_cmd) == 0 or die "Failed to create KPZ: $?";

print "\nDone: $kpz_file\n";
print "Install by uploading to Koha: Plugins > Upload plugin\n";
