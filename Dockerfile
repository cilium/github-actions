FROM docker.io/library/golang:1.17.1@sha256:285cf0cb73ab995caee61b900b2be123cd198f3541ce318c549ea5ff9832bdf0 as builder
LABEL maintainer="maintainer@cilium.io"
ADD . /go/src/github.com/cilium/github-actions
WORKDIR /go/src/github.com/cilium/github-actions
RUN make github-actions
RUN strip github-actions

FROM docker.io/library/alpine:3.14.0@sha256:adab3844f497ab9171f070d4cae4114b5aec565ac772e2f2579405b78be67c96 as certs
RUN apk --update add ca-certificates

FROM scratch
LABEL maintainer="maintainer@cilium.io"
COPY --from=builder /go/src/github.com/cilium/github-actions/github-actions /usr/bin/github-actions
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/usr/bin/github-actions"]
