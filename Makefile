GO ?= go
GO_ENV = GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off

.PHONY: all build test clean install uninstall

all: build

build:
	mkdir -p bin
	$(GO_ENV) $(GO) build -mod=readonly -o bin/easytv ./cmd/easytv

test:
	$(GO_ENV) $(GO) test -mod=readonly ./...
	bash tests/install_test.sh

clean:
	rm -f bin/easytv

install:
	bash scripts/install.sh

uninstall:
	bash scripts/uninstall.sh
