FROM golang:1.25.4-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN go build -o /out/provisioner ./cmd/provisioner

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/provisioner /usr/local/bin/provisioner
USER app
ENTRYPOINT ["/usr/local/bin/provisioner"]
