BINARY_NAME := attach-guard
VERSION := 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

.PHONY: build test vet plugin-build plugin-clean

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/attach-guard

test:
	go test ./...

vet:
	go vet ./...

plugin-build:
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
