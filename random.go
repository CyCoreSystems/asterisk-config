package main

import "github.com/nats-io/nuid"

var secretGenerator *nuid.NUID

func init() {
	secretGenerator = nuid.New()
}

func genSecret() string {
	return secretGenerator.Next()
}
