.PHONY: build test clean

BINDIR := bin
DAEMON := $(BINDIR)/srvrmgrd
CLI := $(BINDIR)/srvrmgr

build: $(DAEMON) $(CLI)

$(DAEMON): cmd/srvrmgrd/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgrd

$(CLI): cmd/srvrmgr/main.go internal/**/*.go
	go build -o $@ ./cmd/srvrmgr

test:
	go test -v ./...

clean:
	rm -rf $(BINDIR)
