ifndef VERSION
VERSION=latest
endif

all:
	docker buildx build -t quay.io/cilium/github-actions:${VERSION} . -f Dockerfile -o type=docker
	@echo -e "\nTo push to the registry:\ndocker push quay.io/cilium/github-actions:${VERSION}"

.PHONY: all github-actions local

github-actions:
	CGO_ENABLED=0 GOOS=linux go build -mod=vendor -a -installsuffix cgo -o $@ ./cmd/...

local: github-actions
	strip github-actions

clean:
	rm -fr github-actions
