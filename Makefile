MIRRORREG_BIN := bin/mirror-registry

GO ?= go
GO_MD2MAN ?= go-md2man

VERSION	:= $(shell cat VERSION)
USE_VENDOR =
LOCAL_LDFLAGS = -buildmode=pie -ldflags "-X=main.Version=$(VERSION)"

.PHONY: all build vendor
all: dep build

dep: ## Get the dependencies
	@$(GO) get -v -d ./...

update: ## Get and update the dependencies
	@$(GO) get -v -d -u ./...

tidy: ## Clean up dependencies
	@$(GO) mod tidy

vendor: dep ## Create vendor directory
	@$(GO) mod vendor

build: ## Build the binary files
	$(GO) build -v -o $(MIRRORREG_BIN) $(USE_VENDOR) $(LOCAL_LDFLAGS) ./cmd

clean: ## Remove previous builds
	@rm -f $(MIRRORREG_BIN)

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'


.PHONY: release
release: ## create release package from git
	git clone https://github.com/thkukuk/mirror-registry
	rm -rf mirror-registry/.git*
	mv mirror-registry mirror-registry-$(VERSION)
	sed -i -e 's|USE_VENDOR =|USE_VENDOR = -mod vendor|g' mirror-registry-$(VERSION)/Makefile
	make -C mirror-registry-$(VERSION) vendor
	tar cJf mirror-registry-$(VERSION).tar.xz mirror-registry-$(VERSION)
	rm -rf mirror-registry-$(VERSION)

#MANPAGES_MD := $(wildcard docs/man/*.md)
#MANPAGES    := $(MANPAGES_MD:%.md=%)

#docs/man/%.1: docs/man/%.1.md
#        $(GO_MD2MAN) -in $< -out $@

#.PHONY: docs
#docs: $(MANPAGES)

#.PHONY: install
#install:
#	$(GO) install $(LOCAL_LDFLAGS) ./cmd/...
