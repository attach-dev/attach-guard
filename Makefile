BINARY_NAME := attach-guard
VERSION := 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: build test vet plugin-build plugin-clean plugin-stamp-version release

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/attach-guard

test:
	go test ./...

vet:
	go vet ./...

plugin-stamp-version:
	@perl -i -pe 's/"version": "[^"]*"/"version": "$(VERSION)"/' plugin/.claude-plugin/plugin.json

plugin-build: plugin-stamp-version
	@mkdir -p plugin/hooks/bin
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		echo "Building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch go build -ldflags="$(LDFLAGS)" \
			-o plugin/hooks/bin/$(BINARY_NAME)-$$os-$$arch ./cmd/attach-guard; \
	done
	@echo "Plugin binaries built in plugin/hooks/bin/"

plugin-clean:
	rm -rf plugin/hooks/bin/

SHA256 := $(shell command -v sha256sum 2>/dev/null || echo "shasum -a 256")

release: test vet plugin-build
	cd plugin/hooks/bin && $(SHA256) attach-guard-* > checksums.txt
