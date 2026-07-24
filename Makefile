# Makefile for koha-rfid-go — build, deploy, package
#
# Targets:
#   make build           — cross-compile Go binaries (linux + windows)
#   make build-linux     — linux only
#   make build-windows   — windows only
#   make deploy-js       — SCP koha-rfid.js to Koha server + restart plack
#   make deploy-plugin   — SCP RFID.pm to Koha server + restart plack
#   make deploy          — deploy-js + deploy-plugin
#   make kpz             — build Koha plugin KPZ package
#   make clean           — remove build artifacts
#   make help            — show this help
#
# The build targets call build.sh under the hood.
# Deploy targets call deploy.sh and deploy-plugin.sh.

VERSION ?= 1.0.0
BUILD_SH := ./build.sh
DEPLOY_SH := ./deploy.sh
DEPLOY_PLUGIN_SH := ./deploy-plugin.sh

.PHONY: all build build-linux build-windows deploy deploy-js deploy-plugin kpz clean help

all: help

# ─── Build Go binaries ──────────────────────────────────

build:
	$(BUILD_SH)

build-linux:
	$(BUILD_SH) linux

build-windows:
	$(BUILD_SH) windows

# ─── Deploy JS to Koha ──────────────────────────────────

deploy-js:
	$(DEPLOY_SH)

deploy-plugin:
	$(DEPLOY_PLUGIN_SH)

deploy: deploy-js deploy-plugin
	@echo "=== Full deploy complete ==="

# ─── Package KPZ ────────────────────────────────────────

kpz:
	cd plugin && perl Makefile.pl

# ─── Clean ──────────────────────────────────────────────

clean:
	$(BUILD_SH) clean
	cd plugin && rm -f *.kpz

# ─── Help ───────────────────────────────────────────────

help:
	@echo "=== koha-rfid-go Makefile ==="
	@echo ""
	@echo "Targets:"
	@echo "  make build           — cross-compile Go binaries (linux + windows)"
	@echo "  make build-linux     — linux only"
	@echo "  make build-windows   — windows only"
	@echo "  make deploy-js       — SCP koha-rfid.js to Koha server"
	@echo "  make deploy-plugin   — SCP RFID.pm to Koha server"
	@echo "  make deploy          — deploy-js + deploy-plugin"
	@echo "  make kpz             — build Koha plugin KPZ package"
	@echo "  make clean           — remove build artifacts"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=x.y.z  — version tag for ldflags (default: 1.0.0)"
	@echo ""
	@echo "Workflow:"
	@echo "  1. Edit koha-rfid.js or server.go"
	@echo "  2. make build        — compile server + CLI tools"
	@echo "  3. make deploy       — push JS + plugin to Koha server"
	@echo "  4. ./server.sh start — start RFID server"
	@echo ""
	@echo "See README.md for full documentation."
