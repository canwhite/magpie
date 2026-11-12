# The recipes (clean's rm -rf too) name paths relative to the working
# folder, so they run only in magpie's own, known by its go.mod: run from
# another one (make -f path/to/Makefile) they would act there. make -C
# works. go.mod is read relative to the working folder, so a path with
# spaces (or a symlinked Makefile) changes nothing.
ifneq ($(shell sed -n 's/^module //p' go.mod 2>/dev/null),github.com/yetone/magpie)
  $(error run make in magpie's folder (make -C path/to/magpie), not in $(CURDIR))
endif

# The accounts and gateway key this machine's targets act on are not in
# this file: they are in .env, which git ignores, so no address is
# committed. The targets that need one say which is unset.
-include .env

# A tag may hold $, ( or `, which the shell lines below would run: the
# version keeps only what a version is spelled with.
VERSION ?= $(or $(shell git describe --tags --always --dirty 2>/dev/null | tr -cd 'A-Za-z0-9._+-'),dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
TAGS     = production
TARGETS  = darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64 android/arm64

# The GUI links the platform webview through cgo, so it is built natively.
# `nogui` builds the terminal-only magpie, which cross-compiles anywhere.
# macOS 12 is the oldest Go (1.25 and later) runs on; Info.plist says so too.
ifeq ($(shell uname -s),Darwin)
  export CGO_CFLAGS  = -mmacosx-version-min=12.0
  export CGO_LDFLAGS = -mmacosx-version-min=12.0
endif
# On Linux, GTK 3 and WebKitGTK 4.1: older distributions have them, where
# Wails' default GTK 4 and WebKitGTK 6 would leave them out.
ifeq ($(shell uname -s),Linux)
  TAGS += gtk3
endif
# Termux uses Android's libc and has no desktop webview. Keep the web UI
# and terminal commands, without linking GTK or Wails.
ifeq ($(shell go env GOOS),android)
  TAGS = nogui
endif

.PHONY: help build cli install test test-ui app icons release release-cli release-windows release-linux clean dev dev-once serve multicodex big-account hybrid close limit limit-list refresh-key key-accounts operation-setup operation code

# The targets this file offers. Each line below ends with ## and what the
# target does, which this prints; a target added without one is left out.
help: ## list these targets
	@awk 'BEGIN { print "usage: make <target>"; print ""; print "targets:" } \
	  /^[a-zA-Z0-9_-]+:.*## / { t = $$0; sub(":.*", "", t); d = $$0; sub("^[^#]*## ", "", d); printf "  %-20s %s\n", t, d }' \
	  $(MAKEFILE_LIST)
	@printf '\n%s\n' "gateway key limits:"
	@printf '  %s\n' \
	  "make limit-list" \
	  "        what is left of the 5-hour and weekly limits of GATEWAY_KEY," \
	  "        and when each window next counts from zero" \
	  "make limit LIMIT_WINDOW=5h LIMIT_TOKENS=50000000" \
	  "        set the token limit of one window (5h, week, day, 30d and so on);" \
	  "        a window that already has a limit is changed, not added to" \
	  "make limit LIMIT_WINDOW=week LIMIT_TOKENS=500000000" \
	  "        the weekly ceiling, the way it was raised to 500M" \
	  "make limit LIMIT_ADD=1 LIMIT_WINDOW=day LIMIT_TOKENS=10000000" \
	  "        add a limit to a window that has none" \
	  "make limit GATEWAY_KEY=<id> ..." \
	  "        the same for another key"

build: ## build magpie with its GUI (native, cgo)
	go build -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" -o magpie .

cli: ## build the terminal-only magpie (cross-compiles anywhere)
	CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o magpie .

install: ## install magpie onto GOPATH/bin
	go install -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" .

test: ## go vet and go test every package
	go vet -tags "$(TAGS)" ./... && go test -tags "$(TAGS)" ./...

# The browser regressions under internal/gui/tests: the real pages in
# Playwright's Chromium and WebKit; see internal/gui/tests/README.md.
UI_TESTS = $(wildcard internal/gui/tests/*.test.cjs)
# Files are independent and run in parallel; 1 diagnoses a flaky one.
UI_TEST_CONCURRENCY ?= 2

test-ui: ## run the GUI's browser tests (Playwright)
	@test -n "$(UI_TESTS)" || { echo "no GUI tests found under internal/gui/tests" >&2; exit 1; }
	node --test --test-concurrency=$(UI_TEST_CONCURRENCY) $(UI_TESTS)

# macOS bundle: menu bar app with no Dock icon (LSUIElement).
app: build ## build the macOS app bundle (magpie.app)
	@rm -rf magpie.app
	@mkdir -p magpie.app/Contents/MacOS magpie.app/Contents/Resources
	@cp magpie magpie.app/Contents/MacOS/magpie
	@cp build/darwin/magpie.icns magpie.app/Contents/Resources/magpie.icns
	@cp build/darwin/Assets.car magpie.app/Contents/Resources/Assets.car
	@sed 's/@VERSION@/$(VERSION)/' build/darwin/Info.plist > magpie.app/Contents/Info.plist
	@echo "  magpie.app"

icons: ## regenerate the tray and app icons
	@go run build/icon/gen.go tray internal/gui/tray.png
	@go run build/icon/gen.go tray-flap internal/gui/trayflap
	@go run build/icon/gen.go app 64 internal/gui/icon.png
	@go run build/icon/gen.go app 1024 internal/gui/icon-1024.png
	@go run build/icon/gen.go app 256 internal/gui/icon-256.png
	@rm -rf build/darwin/magpie.iconset && mkdir -p build/darwin/magpie.iconset
	@for s in 16 32 128 256 512; do \
		go run build/icon/gen.go app $$s build/darwin/magpie.iconset/icon_$${s}x$${s}.png; \
		go run build/icon/gen.go app $$((s*2)) build/darwin/magpie.iconset/icon_$${s}x$${s}@2x.png; \
	done
	@iconutil -c icns build/darwin/magpie.iconset -o build/darwin/magpie.icns && rm -rf build/darwin/magpie.iconset
	@# macOS 26 draws the Dock icon from AppIcon.icon, light and dark (Xcode 26's actool);
	@# the compiled Assets.car is kept in the tree so a release needn't have it
	@go run build/icon/gen.go glyph 1024 build/darwin/AppIcon.icon/Assets/magpie.png
	@rm -rf build/darwin/actool && mkdir -p build/darwin/actool
	@xcrun actool build/darwin/AppIcon.icon --compile build/darwin/actool --platform macosx \
		--minimum-deployment-target 11.0 --app-icon AppIcon --output-partial-info-plist build/darwin/actool/partial.plist >/dev/null
	@mv build/darwin/actool/Assets.car build/darwin/Assets.car && rm -rf build/darwin/actool

release: clean build ## build the desktop app and the CLI binaries into dist
	@mkdir -p dist
	@cp magpie dist/magpie-$(shell go env GOOS)-$(shell go env GOARCH)
	@$(MAKE) --no-print-directory release-cli

release-cli: ## build the terminal-only binaries for every platform into dist
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ $$os = windows ] && ext=.exe; \
		echo "  $$os/$$arch (cli)"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o dist/magpie-cli-$$os-$$arch$$ext . || exit 1; \
	done

# The desktop app for Windows: the system WebView2 needs no cgo, so both
# arches cross-compile. Linked as a GUI program (no console window), with
# the icon, version and a DPI-aware manifest from build/windows.
WINVER = $(shell echo $(VERSION) | sed -E 's/^v//; s/[^0-9.].*//')
release-windows: ## build the Windows desktop app into dist
	@mkdir -p dist
	@go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64,arm64 --out rsrc \
		--file-version "$(or $(WINVER),0.0.0)" --product-version "$(or $(WINVER),0.0.0)"
	@for arch in amd64 arm64; do \
		echo "  windows/$$arch (app)"; \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch go build -tags production -trimpath -ldflags="$(LDFLAGS) -H windowsgui" -o dist/magpie-windows-$$arch.exe . || exit 1; \
	done; rm -f rsrc_windows_*.syso

# The desktop app for Linux links GTK 3 and WebKitGTK 4.1 through cgo, so it
# is built natively, one arch per machine.
release-linux: ## build the Linux desktop app into dist
	@mkdir -p dist
	@echo "  linux/$(shell go env GOARCH) (app)"
	@go build -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" -o dist/magpie-linux-$(shell go env GOARCH) .

clean: ## remove built binaries and bundles
	rm -rf magpie magpie.exe magpie.app dist rsrc_windows_*.syso

# Startup: the gateway this machine serves, in the foreground (Ctrl+C
# stops it), on SERVE_ADDR as the launchd service ran it. `serve` starts
# with the accounts as they stand. Every Codex account magpie serves is
# one it signed in itself, kept in logins.json; a Codex folder's sign-in
# is the client's, read and never switched, so what ~/.codex holds no
# longer says which account is in use. The three starts below put one
# group of magpie's own accounts in use and the rest out, then serve:
# `multicodex` multicodex's four, `big-account` the machine's own one,
# `hybrid` both groups together.
SERVE_ADDR ?= 0.0.0.0:3425

# The shares the gateway moves its requests at, from the same .env as the
# groups below. They are handed to `magpie serve` as the environment, which
# wins over ~/.config/magpie/.env, so a start from here follows this file.
# A magpie started any other way reads only ~/.config/magpie/.env.
MAGPIE_SWITCH_LEFT ?= 15
MAGPIE_SWITCH_LEFT_LONG ?= 2
SWITCH_SHARES = MAGPIE_SWITCH_LEFT=$(MAGPIE_SWITCH_LEFT) MAGPIE_SWITCH_LEFT_LONG=$(MAGPIE_SWITCH_LEFT_LONG)

# Each group is a list of names magpie keeps the accounts under, set in
# .env: MULTICODEX_ACCOUNTS holds multicodex's four profiles' accounts and
# MACHINE_ACCOUNT the machine's own account from before.
serve: cli ## run the gateway in the foreground (SERVE_ADDR, default 0.0.0.0:3425)
	$(SWITCH_SHARES) MAGPIE_ADDR=$(SERVE_ADDR) ./magpie serve

multicodex: cli ## run the gateway with multicodex's four accounts in use
	@test -n "$(strip $(MULTICODEX_ACCOUNTS))" || { echo "  MULTICODEX_ACCOUNTS is empty: set it in .env" >&2; exit 1; }
	@for u in $(MULTICODEX_ACCOUNTS); do ./magpie accounts on codex "$$u" || exit 1; done
	@./magpie accounts off codex "$(MACHINE_ACCOUNT)"
	$(SWITCH_SHARES) MAGPIE_ADDR=$(SERVE_ADDR) ./magpie serve

big-account: cli ## run the gateway with the machine's own account in use
	@test -n "$(strip $(MACHINE_ACCOUNT))" || { echo "  MACHINE_ACCOUNT is empty: set it in .env" >&2; exit 1; }
	@./magpie accounts on codex "$(MACHINE_ACCOUNT)"
	@for u in $(MULTICODEX_ACCOUNTS); do ./magpie accounts off codex "$$u" || exit 1; done
	$(SWITCH_SHARES) MAGPIE_ADDR=$(SERVE_ADDR) ./magpie serve

hybrid: cli ## run the gateway with both account groups in use
	@test -n "$(strip $(MULTICODEX_ACCOUNTS))" || { echo "  MULTICODEX_ACCOUNTS is empty: set it in .env" >&2; exit 1; }
	@test -n "$(strip $(MACHINE_ACCOUNT))" || { echo "  MACHINE_ACCOUNT is empty: set it in .env" >&2; exit 1; }
	@for u in $(MULTICODEX_ACCOUNTS) $(MACHINE_ACCOUNT); do ./magpie accounts on codex "$$u" || exit 1; done
	$(SWITCH_SHARES) MAGPIE_ADDR=$(SERVE_ADDR) ./magpie serve

# A gateway key's own limits. The key is GATEWAY_KEY, set in .env. The
# window a limit sits in is named the way the CLI names it: day, week,
# month, 5h, 7d and so on. Setting the tokens of a window that already
# has a limit changes that limit and leaves the others alone, and it
# does not restart the window: the count so far stays, only the ceiling
# moves. A window that has no limit yet needs `add` before it
# (LIMIT_ADD=1).
LIMIT_WINDOW ?= 5h
LIMIT_TOKENS ?= 50000000

limit: cli ## set a key's limit (GATEWAY_KEY, LIMIT_WINDOW, LIMIT_TOKENS, LIMIT_ADD=1)
	@test -n "$(strip $(GATEWAY_KEY))" || { echo "  GATEWAY_KEY is empty: set it in .env" >&2; exit 1; }
	./magpie gateway-key limit $(GATEWAY_KEY) $(if $(LIMIT_ADD),add )$(LIMIT_WINDOW) --tokens $(LIMIT_TOKENS)

limit-list: cli ## show what is left of a key's limits, and when each resets (GATEWAY_KEY)
	@test -n "$(strip $(GATEWAY_KEY))" || { echo "  GATEWAY_KEY is empty: set it in .env" >&2; exit 1; }
	@build/limit-list.sh ./magpie $(GATEWAY_KEY)

refresh-key: cli ## rotate a gateway key, and print the new one (GATEWAY_KEY)
	@test -n "$(strip $(GATEWAY_KEY))" || { echo "  GATEWAY_KEY is empty: set it in .env" >&2; exit 1; }
	./magpie gateway-key rotate $(GATEWAY_KEY)

# The accounts GATEWAY_KEY may spend (#905), set in .env as
# "<provider>/<account>" entries: one account, by the stable id
# caller-keys.json keeps, or every account when the list is "all". A key
# held to a list can use only accounts that are both on and on it, so one
# list holding every group lets the same key serve whichever group
# `multicodex`, `big-account` or `hybrid` puts on.
GATEWAY_KEY_ACCOUNTS ?=

key-accounts: cli ## set GATEWAY_KEY's accounts from .env (GATEWAY_KEY_ACCOUNTS)
	@test -n "$(strip $(GATEWAY_KEY))" || { echo "  GATEWAY_KEY is empty: set it in .env" >&2; exit 1; }
	@test -n "$(strip $(GATEWAY_KEY_ACCOUNTS))" || { echo "  GATEWAY_KEY_ACCOUNTS is empty: set it in .env" >&2; exit 1; }
	./magpie gateway-key accounts $(GATEWAY_KEY) $(GATEWAY_KEY_ACCOUNTS)
	@./magpie gateway-key accounts $(GATEWAY_KEY)

# `operation` is a separate LAN gateway. Its XDG roots keep its provider,
# caller key and usage out of the gateway on SERVE_ADDR. It discovers no
# subscription accounts, so only its OpenAI API provider can serve requests.
OPERATION_ADDR ?= 0.0.0.0:5555
OPERATION_CONFIG_HOME ?= $(CURDIR)/.operation-config
OPERATION_CACHE_HOME ?= $(CURDIR)/.operation-cache
OPERATION_OPENAI_API_KEY ?=
OPEKEY ?=
OPERATION_ENV = XDG_CONFIG_HOME="$(OPERATION_CONFIG_HOME)" XDG_CACHE_HOME="$(OPERATION_CACHE_HOME)" MAGPIE_NO_ACCOUNTS=1

operation-setup: cli ## configure isolated OpenAI Luna gateway (OPERATION_OPENAI_API_KEY, OPEKEY)
	@test -n "$(strip $(OPERATION_OPENAI_API_KEY))" || { echo "  OPERATION_OPENAI_API_KEY is empty: set it in .env" >&2; exit 1; }
	@test -n "$(strip $(OPEKEY))" || { echo "  OPEKEY is empty: set it in .env" >&2; exit 1; }
	@mkdir -p "$(OPERATION_CONFIG_HOME)" "$(OPERATION_CACHE_HOME)"
	@$(OPERATION_ENV) ./magpie provider openai >/dev/null 2>&1 || $(OPERATION_ENV) ./magpie provider add openai "$(OPERATION_OPENAI_API_KEY)" >/dev/null
	@$(OPERATION_ENV) ./magpie provider key openai "$(OPERATION_OPENAI_API_KEY)" >/dev/null
	@$(OPERATION_ENV) ./magpie provider models openai gpt-5.6-luna >/dev/null
	@key_id="$$(OPEKEY="$(OPEKEY)" python3 -c 'import json, os, sys; p=sys.argv[1]; keys=json.load(open(p)) if os.path.exists(p) else []; print(next((key["id"] for key in keys if key.get("secret") == os.environ["OPEKEY"]), ""))' "$(OPERATION_CONFIG_HOME)/magpie/caller-keys.json")"; \
	if [ -z "$$key_id" ]; then printf '%s\n' "$(OPEKEY)" | $(OPERATION_ENV) ./magpie gateway-key add opekey --key - >/dev/null; \
	key_id="$$(OPEKEY="$(OPEKEY)" python3 -c 'import json, os, sys; keys=json.load(open(sys.argv[1])); print(next(key["id"] for key in keys if key.get("secret") == os.environ["OPEKEY"]))' "$(OPERATION_CONFIG_HOME)/magpie/caller-keys.json")"; fi; \
	$(OPERATION_ENV) ./magpie gateway-key models "$$key_id" openai/gpt-5.6-luna >/dev/null; \
	$(OPERATION_ENV) ./magpie gateway-key models "$$key_id"

operation: operation-setup ## run isolated OpenAI Luna gateway on OPERATION_ADDR (default 0.0.0.0:5555)
	$(OPERATION_ENV) MAGPIE_ADDR=$(OPERATION_ADDR) ./magpie serve

# The "Code" routing group: OpenCode Go's GLM 5.3 Flash first, then
# MiniMax's M3.1 Flash, with routing=order, so the first model answers
# until it can't and the next one takes over. An agent picks the group as
# group/code. The pair is named here so another one needs no edit to the
# recipe: make code CODE_MODELS="opencode-go/glm-5.3,minimax/MiniMax-M3".
CODE_MODELS ?= opencode-go/glm-5.3-flash,minimax/MiniMax-M3.1-Flash
CODE_ROUTING ?= order

code: cli ## make the "Code" routing group (CODE_MODELS, CODE_ROUTING)
	./magpie group add "Code" models=$(CODE_MODELS) routing=$(CODE_ROUTING)
	./magpie group code

# Stop every magpie service this machine runs: the gateway on SERVE_ADDR
# (whichever of the starts above brought it up, or a run by hand) and the
# launchd service when its plist is loaded. The gateway is asked to stop
# first, and killed if it is still there after ten seconds.
close: ## stop the gateway and the launchd service
	@launchctl bootout gui/$$(id -u)/com.yetone.magpie.serve 2>/dev/null || true
	@addr=$(SERVE_ADDR); port=$${addr##*:}; \
	pids=$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null); \
	if [ -z "$$pids" ]; then echo "  nothing listens on $$port"; else \
	  kill -TERM $$pids 2>/dev/null || true; \
	  i=0; while [ $$i -lt 10 ] && [ -n "$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null)" ]; do sleep 1; i=$$((i+1)); done; \
	  if [ -n "$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null)" ]; then kill -KILL $$pids 2>/dev/null || true; fi; \
	  echo "  gateway on $$port stopped"; \
	fi

# Development: the UI is served from internal/gui/assets and the window
# reloads itself when a file there is saved. With fswatch installed, the
# windows stay up and a Go change rebuilds and restarts only the backend
# behind them (build/dev.sh); without it, dev-once runs the app as one
# process. Uses its own ports so a running magpie keeps serving the agents.
DEV_ADDR ?= 127.0.0.1:3426
DEV_UI ?= 127.0.0.1:3427
DEV_BACKEND ?= 127.0.0.1:3428
DEV_CONTROL ?= 127.0.0.1:3429
dev: ## run the GUI with live reload, on its own ports
	@if command -v fswatch >/dev/null; then \
	  MAGPIE_ADDR=$(DEV_ADDR) MAGPIE_DEV_UI=$(DEV_UI) MAGPIE_DEV_BACKEND=$(DEV_BACKEND) MAGPIE_DEV_CONTROL=$(DEV_CONTROL) build/dev.sh; \
	else $(MAKE) --no-print-directory dev-once; fi

dev-once: ## run one development build of the GUI, without live reload
	@go build -tags dev -o magpie-dev . && echo "  magpie-dev · UI from internal/gui/assets, reload on save · gateway $(DEV_ADDR)"
	@MAGPIE_ADDR=$(DEV_ADDR) MAGPIE_DEV_UI=$(DEV_UI) ./magpie-dev app
