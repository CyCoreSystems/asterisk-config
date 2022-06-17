FROM golang:1.17 AS builder
WORKDIR /go/src
COPY . .
RUN go get -d -v
RUN CGO_ENABLED=0 go build -o /go/bin/app

FROM gcr.io/distroless/static
COPY defaults /defaults
COPY --from=builder /go/bin/app /go/bin/app
ENTRYPOINT ["/go/bin/app"]
