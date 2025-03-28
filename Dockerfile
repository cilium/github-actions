FROM docker.io/library/golang:1.21.0@sha256:b490ae1f0ece153648dd3c5d25be59a63f966b5f9e1311245c947de4506981aa as builder
LABEL maintainer="maintainer@cilium.io"
ADD . /go/src/github.com/cilium/github-actions
WORKDIR /go/src/github.com/cilium/github-actions
RUN make github-actions
RUN strip github-actions

FROM docker.io/library/alpine:3.21.3@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c as certs
RUN apk --update add ca-certificates

FROM scratch
LABEL maintainer="maintainer@cilium.io"
COPY --from=builder /go/src/github.com/cilium/github-actions/github-actions /usr/bin/github-actions
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/usr/bin/github-actions"]
