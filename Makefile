#!/usr/bin/make -f

SHELL=bash
# GOBIN is used only to install neo-go and allows to override
# the location of written binary.
export GOBIN ?= $(shell pwd)/bin
export CGO_ENABLED=0
NEOGO ?= $(GOBIN)/cli
VERSION ?= $(shell git describe --tags --dirty --match "v*" --always --abbrev=8 2>/dev/null || cat VERSION 2>/dev/null || echo "develop")
NEOGOORIGMOD = github.com/nspcc-dev/neo-go@v0.102.0
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
morph_sc = audit balance container neofsid netmap proxy reputation subnet
mainnet_sc = neofs processing
nns_sc = nns

define sc_template
$(2)$(1)/$(1)_contract.nef: $(2)$(1)/$(1)_contract.go
	$(NEOGO) contract compile -i $(2)$(1) -c $(if $(2),$(2),$(1)/)config.yml -m $(2)$(1)/config.json -o $(2)$(1)/$(1)_contract.nef --bindings $(2)$(1)/bindings_config.yml
	mkdir -p rpc/$(1)
	$(NEOGO) contract generate-rpcwrapper -o rpc/$(1)/rpcbinding.go -m $(2)$(1)/config.json --config $(2)$(1)/bindings_config.yml

$(if $(2),$(2)$(1)/$(1)_contract.go: alphabet/alphabet.go alphabet/alphabet.tpl
	go run alphabet/alphabet.go
)
endef

$(foreach sc,$(alphabet_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(morph_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(mainnet_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(nns_sc),$(eval $(call sc_template,$(sc))))

alphabet: $(foreach sc,$(alphabet_sc),$(sc)/$(sc)_contract.nef)
morph: $(foreach sc,$(morph_sc),$(sc)/$(sc)_contract.nef)
mainnet: $(foreach sc,$(mainnet_sc),$(sc)/$(sc)_contract.nef)
nns: $(foreach sc,$(nns_sc),$(sc)/$(sc)_contract.nef)

neo-go:
	@go install -trimpath -v -ldflags "-X '$(NEOGOMOD)/pkg/config.Version=$(NEOGOVER)'" $(NEOGOMOD)/cli@v$(NEOGOVER)

test:
	@go test ./...

clean:
	find . -name '*.nef' -exec rm -rf {} \;
	find . -name 'config.json' -exec rm -rf {} \;
	find . -name 'bindings_config.yml' -exec rm -rf {} \;
	rm -rf ./bin/

mr_proper: clean
	for sc in $(alphabet_sc); do\
	  rm -rf alphabet/$$sc; \
	done

archive: build
	@tar --transform "s|^./|neofs-contract-$(VERSION)/|" \
		-czf neofs-contract-$(VERSION).tar.gz \
		$(shell find . -name '*.nef' -o -name 'config.json')

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
