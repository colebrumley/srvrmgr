.PHONY: build test clean download-model

BINDIR := bin
DAEMON := $(BINDIR)/srvrmgrd
CLI := $(BINDIR)/srvrmgr
MODEL := internal/embedder/models/model.onnx
MODEL_URL := https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx

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

clean:
	rm -rf $(BINDIR)
