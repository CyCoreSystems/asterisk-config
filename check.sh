#!/bin/bash -e
#dep ensure
#gometalinter --skip test --skip defaults --cyclo-over=15 --deadline=90s --vendor ./...
golangci-lint run
go test ./...
go build ./...
go build
