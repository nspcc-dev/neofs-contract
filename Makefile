#!/usr/bin/make -f

SHELL=bash
# GOBIN is used only to install neo-go and allows to override
# the location of written binary.
export GOBIN ?= $(shell pwd)/bin
export CGO_ENABLED=0
NEOGO ?= $(GOBIN)/cli
VERSION ?= $(shell git describe --tags --dirty --match "v*" --always --abbrev=8 2>/dev/null || cat VERSION 2>/dev/null || echo "develop")
NEOGOORIGMOD = github.com/nspcc-dev/neo-go@v0.104.0
NEOGOMOD = $(shell go list -f '{{.Path}}' -m $(NEOGOORIGMOD))
NEOGOVER = $(shell go list -f '{{.Version}}' -m $(NEOGOORIGMOD) | tr -d v)

# .deb package versioning
OS_RELEASE = $(shell lsb_release -cs)
PKG_VERSION ?= $(shell echo $(VERSION) | sed "s/^v//" | \
			sed -E "s/(.*)-(g[a-fA-F0-9]{6,8})(.*)/\1\3~\2/" | \
			sed "s/-/~/")-${OS_RELEASE}

.PHONY: all build clean test neo-go
.PHONY: alphabet mainnet morph nns sidechain
.PHONY: debpackage debclean
build: neo-go all
all: sidechain mainnet
sidechain: alphabet morph nns

alphabet_sc = alphabet
morph_sc = audit balance container neofsid netmap proxy reputation
mainnet_sc = neofs processing
nns_sc = nns

all_sc = $(alphabet_sc) $(morph_sc) $(mainnet_sc) $(nns_sc)

%/contract.nef %/bindings_config.yml %/config.json: $(NEOGO) %/contract.go %/config.yml
	$(NEOGO) contract compile -i $* -c $*/config.yml -m $*/config.json -o $*/contract.nef --bindings $*/bindings_config.yml

rpc/%/rpcbinding.go: contracts/%/config.json contracts/%/bindings_config.yml
	mkdir -p rpc/$*
	$(NEOGO) contract generate-rpcwrapper -o rpc/$*/rpcbinding.go -m contracts/$*/config.json --config contracts/$*/bindings_config.yml

alphabet: $(foreach sc,$(alphabet_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json rpc/$(sc)/rpcbinding.go)
morph: $(foreach sc,$(morph_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json rpc/$(sc)/rpcbinding.go)
mainnet: $(foreach sc,$(mainnet_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json rpc/$(sc)/rpcbinding.go)
nns: $(foreach sc,$(nns_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json rpc/$(sc)/rpcbinding.go)

neo-go: $(NEOGO)

$(NEOGO): Makefile
	@go install -trimpath -v -ldflags "-X '$(NEOGOMOD)/pkg/config.Version=$(NEOGOVER)'" $(NEOGOMOD)/cli@v$(NEOGOVER)

test:
	@go test ./...

clean:
	rm -rf ./bin $(foreach sc,$(all_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json contracts/$(sc)/bindings_config.yml)

archive: neofs-contract-$(VERSION).tar.gz

neofs-contract-$(VERSION).tar.gz: $(foreach sc,$(all_sc),contracts/$(sc)/contract.nef contracts/$(sc)/config.json)
	@tar --transform "s|^\(contracts\)/\([a-z]\+\)/\(contract.nef\)$$|\\1/\\2/\\2_\\3|" \
		--transform "s|^contracts/|neofs-contract-$(VERSION)/|" \
		-czf $@ \
		$(shell find contracts -name '*.nef' -o -name 'config.json')

# Package for Debian
debpackage:
	dch --package neofs-contract \
			--controlmaint \
			--newversion $(PKG_VERSION) \
			--distribution $(OS_RELEASE) \
			"Please see CHANGELOG.md for code changes for $(VERSION)"
	dpkg-buildpackage --no-sign -b

debclean:
	dh clean		

fmt:
	@gofmt -l -w -s $$(find . -type f -name '*.go'| grep -v "/vendor/")
