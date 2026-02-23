.PHONY: build test clean download-model install uninstall

BINDIR := bin
DAEMON := $(BINDIR)/srvrmgrd
CLI := $(BINDIR)/srvrmgr
MODEL := internal/embedder/models/model.onnx
MODEL_URL := https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx

PREFIX := /usr/local
PLIST_SRC := install/com.srvrmgr.daemon.plist

# When run under sudo, resolve the real user's UID and home directory
ifdef SUDO_USER
_UID := $(shell id -u $(SUDO_USER))
_HOME := $(shell dscl . -read /Users/$(SUDO_USER) NFSHomeDirectory | awk '{print $$2}')
else
_UID := $(shell id -u)
_HOME := $(HOME)
endif

PLIST_DST := $(_HOME)/Library/LaunchAgents/com.srvrmgr.daemon.plist

build: $(DAEMON) $(CLI)

$(MODEL):
	@echo "Downloading embedding model..."
	@mkdir -p $(dir $(MODEL))
	@curl -L "$(MODEL_URL)" -o $(MODEL)

$(DAEMON): $(MODEL) cmd/srvrmgrd/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgrd

$(CLI): cmd/srvrmgr/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgr

download-model: $(MODEL)

test: $(MODEL)
	go test -v ./...

install: $(DAEMON) $(CLI)
	@echo "Installing binaries to $(PREFIX)/bin..."
	install -d $(PREFIX)/bin
	install -m 755 $(DAEMON) $(PREFIX)/bin/srvrmgrd
	install -m 755 $(CLI) $(PREFIX)/bin/srvrmgr
	@echo "Installing launchd agent..."
	@mkdir -p $(_HOME)/Library/LaunchAgents
	@sed 's|/usr/local/bin/srvrmgrd|$(PREFIX)/bin/srvrmgrd|g' $(PLIST_SRC) > $(PLIST_DST)
	launchctl bootout gui/$(_UID) $(PLIST_DST) 2>/dev/null || true
	launchctl bootstrap gui/$(_UID) $(PLIST_DST)
	@echo "srvrmgr installed and daemon started."

uninstall:
	@echo "Stopping daemon..."
	launchctl bootout gui/$(_UID) $(PLIST_DST) 2>/dev/null || true
	@echo "Removing launchd plist..."
	rm -f $(PLIST_DST)
	@echo "Removing binaries..."
	rm -f $(PREFIX)/bin/srvrmgrd $(PREFIX)/bin/srvrmgr
	@echo "srvrmgr uninstalled."

clean:
	rm -rf $(BINDIR)
