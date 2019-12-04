FROM docker.io/library/golang:1.13.4 as builder
LABEL maintainer="maintainer@cilium.io"
ADD . /go/src/github.com/cilium/github-actions
WORKDIR /go/src/github.com/cilium/github-actions
RUN make github-actions
RUN strip github-actions

FROM docker.io/library/alpine:3.9.3 as certs
RUN apk --update add ca-certificates

FROM scratch
LABEL maintainer="maintainer@cilium.io"
COPY --from=builder /go/src/github.com/cilium/github-actions/github-actions /usr/bin/github-actions
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/usr/bin/github-actions"]
