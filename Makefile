PACKAGE=github.com/nspcc-dev/neofs-contract
NEOGO?=neo-go

.PHONY: build tests

build:
	$(NEOGO) contract compile -i neofs_contract.go

tests:
	go mod vendor
	go test -mod=vendor -v -race $(PACKAGE)
