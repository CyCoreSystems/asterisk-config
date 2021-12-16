FROM golang:1.17 AS builder
COPY . .
RUN go get -d -v
RUN go build -o /go/bin/app

FROM gcr.io/distroless/static
COPY defaults /defaults
COPY --from=builder /go/bin/app /go/bin/app
ENTRYPOINT ["/go/bin/app"]
