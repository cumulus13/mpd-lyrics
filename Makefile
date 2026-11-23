BINARY  := mpd-lyrics
PKG     := ./cmd/mpd-lyrics
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist

# os/arch pairs built by `make cross`
TARGETS := linux/amd64 linux/arm64 linux/arm linux/riscv64 \
           darwin/amd64 darwin/arm64 \
           windows/amd64 windows/arm64 \
           freebsd/amd64 openbsd/amd64 netbsd/amd64

.PHONY: all build test race vet fmt fmt-check check sync-config cross checksums clean install

all: check build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# config.example.toml is a copy of the embedded default config.
sync-config:
	cp internal/config/default.toml config.example.toml

check: fmt-check vet test

cross:
	@mkdir -p $(DIST)
	@set -e; for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
	  echo "building $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
	    -o $(DIST)/$(BINARY)-$(VERSION)-$$os-$$arch$$ext $(PKG); \
	done

checksums:
	cd $(DIST) && (sha256sum * 2>/dev/null || shasum -a 256 *) > SHA256SUMS

clean:
	rm -rf $(DIST) $(BINARY) $(BINARY).exe
