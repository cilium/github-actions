FROM docker.io/library/golang:1.16.3@sha256:f7d3519759ba6988a2b73b5874b17c5958ac7d0aa48a8b1d84d66ef25fa345f1 as builder
LABEL maintainer="maintainer@cilium.io"
ADD . /go/src/github.com/cilium/github-actions
WORKDIR /go/src/github.com/cilium/github-actions
RUN make github-actions
RUN strip github-actions

FROM docker.io/library/alpine:3.12.7@sha256:36553b10a4947067b9fbb7d532951066293a68eae893beba1d9235f7d11a20ad as certs
RUN apk --update add ca-certificates

FROM scratch
LABEL maintainer="maintainer@cilium.io"
COPY --from=builder /go/src/github.com/cilium/github-actions/github-actions /usr/bin/github-actions
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/usr/bin/github-actions"]
