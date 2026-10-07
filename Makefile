# osscycler build and run targets. `make help` lists them.
#
# Local settings (rider weight, address, ...) can go in local.mk, which is
# git-ignored, e.g.:  CORE_FLAGS = -rider-kg 75
# Environment for the core goes there too, exported, e.g. to push metrics:
#   export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT = http://prometheus:9090/api/v1/otlp/v1/metrics
-include local.mk

BIN          ?= _bin
LOG_DIR      ?= _logs
SECRETS      ?= _secrets
ADDR         ?= 127.0.0.1:7420
TOKEN_FILE   ?= $(SECRETS)/api-token
ANT_KEY_FILE ?= $(SECRETS)/ant-network-key
CORE_FLAGS   ?=
PORT         ?= /dev/ttyANT
COURSES      ?= _gpx
WORKOUTS     ?= _workouts
RIDES        ?= _rides

STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.1

DIST      ?= _dist
VERSION   ?= $(shell git describe --tags --always --dirty)

export BIN LOG_DIR ADDR TOKEN_FILE ANT_KEY_FILE PORT COURSES WORKOUTS RIDES

.DEFAULT_GOAL := build
.PHONY: help build generate test race vet fmt lint check token udev \
        core core-fake tui stack stack-fake demo demo-free demo-files replay release clean

help: ## List targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all binaries into $(BIN)/
	go build -o $(BIN)/ ./cmd/...

generate: ## Lint the proto schema and regenerate gen/
	go generate ./

test: ## Run tests
	go test ./...

race: ## Run tests with the race detector
	go test -race -count=1 ./...

vet: ## go vet
	go vet ./...

fmt: ## Fail if any file needs gofmt
	@out=$$(gofmt -l $$(git ls-files '*.go') $$(git ls-files --others --exclude-standard '*.go')); \
	if [ -n "$$out" ]; then echo "needs gofmt:"; echo "$$out"; exit 1; fi

lint: ## staticcheck (pinned, via go run)
	go run $(STATICCHECK) ./...

check: fmt vet lint race ## Everything CI should run

token: $(TOKEN_FILE) ## Create the API token file if missing

$(TOKEN_FILE):
	@mkdir -p $(SECRETS) && chmod 700 $(SECRETS)
	@umask 077 && go run ./cmd/osscycler-core -new-token > $@
	@echo "wrote $@"

udev: ## Install the ANT stick udev rule (sudo)
	sudo install -m 644 deploy/udev/99-ant-usb.rules /etc/udev/rules.d/
	sudo groupadd -f ant
	sudo usermod -aG ant $(USER)
	sudo udevadm control --reload && sudo udevadm trigger
	@echo "replug the stick and log in again for the group to apply"

core: build token ## Run the core against the stick (foreground)
	scripts/run.sh core $(CORE_FLAGS)

core-fake: build token ## Run the core with a synthetic ride (foreground)
	FAKE=1 scripts/run.sh core

tui: build token ## Run the TUI against a running core
	$(BIN)/osscycler-tui -addr $(ADDR) -token-file $(TOKEN_FILE)

stack: build token ## Core (background, log in _logs/core.log) + TUI; quitting the TUI stops the core
	scripts/run.sh stack $(CORE_FLAGS)

stack-fake: build token ## Same as stack, with a synthetic ride
	FAKE=1 scripts/run.sh stack

DEMO_ADDR ?= 127.0.0.1:7429
# Join Demo Hills just below the 9 % crest, so the tour shows grade changes.
# The demo has its own synthetic rider profile, so it never asks for or
# touches yours.
DEMO_FLAGS ?= -ride-start-m 1480 -profile _demo/profile.json

demo: build token demo-files ## Demo: mock rider, demo course and workouts, guided tour (any key takes over)
	FAKE=1 ADDR=$(DEMO_ADDR) LOG_DIR=_demo/logs COURSES=demo/courses WORKOUTS=_demo/workouts RIDES=_demo/rides \
		TUI_FLAGS=-tour scripts/run.sh stack $(DEMO_FLAGS) $(CORE_FLAGS)

demo-free: build token demo-files ## Same as demo without the tour
	FAKE=1 ADDR=$(DEMO_ADDR) LOG_DIR=_demo/logs COURSES=demo/courses WORKOUTS=_demo/workouts RIDES=_demo/rides \
		scripts/run.sh stack $(DEMO_FLAGS) $(CORE_FLAGS)

# Copy the demo workouts into a scratch library, keeping any edits made
# there in earlier demos (cp -n is deprecated in GNU coreutils and
# --update=none doesn't exist on macOS, so check by hand), and give the
# demo rider a complete profile.
demo-files:
	@mkdir -p _demo/workouts
	@[ -e _demo/profile.json ] || printf '{"weight_kg": 75, "ftp_w": 200, "difficulty_pct": 50, "bike_kg": 9}\n' > _demo/profile.json
	@for f in demo/workouts/*.zwo; do \
		[ -e "_demo/workouts/$${f##*/}" ] || cp "$$f" _demo/workouts/; \
	done

replay: build ## Re-ride recorded course rides from $(RIDES) (courses from $(COURSES)); add REPLAY_FLAGS="-cda 0.25" etc.
	$(BIN)/osscycler-replay -rides $(RIDES) -courses $(COURSES) $(REPLAY_FLAGS)

# Release builds with the ANT+ network key built in (see scripts/release.sh):
# Linux tar.gz, .deb and .rpm, and a macOS universal tar.gz.
release: ## Release archives and packages in $(DIST)/, ANT+ key built in
	DIST=$(DIST) VERSION=$(VERSION) ANT_KEY_FILE=$(ANT_KEY_FILE) scripts/release.sh

clean: ## Remove binaries, logs, release archives and demo scratch files
	rm -rf $(BIN) $(LOG_DIR) $(DIST) _demo
