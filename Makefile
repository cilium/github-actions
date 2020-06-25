ifndef VERSION
VERSION=latest
endif

all:
	docker build -t cilium/github-actions:${VERSION} .
	@echo -e "\nTo push to the registry:\ndocker push cilium/github-actions:${VERSION}"

.PHONY: all github-actions local

github-actions:
	CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -installsuffix cgo -o $@ ./cmd/...

local: github-actions
	strip github-actions

clean: rm -fr github-actions
