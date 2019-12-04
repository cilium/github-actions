ifndef VERSION
VERSION=latest
endif

all:
	docker build -t cilium/github-actions:${VERSION} .
	@echo "\nTo push to the registry:\ndocker push cilium/github-actions:${VERSION}"

github-actions:
	CGO_ENABLED=0 GOOS=linux go build -mod=vendor $(GOBUILD) -a -installsuffix cgo -o $@ ./cmd/...
