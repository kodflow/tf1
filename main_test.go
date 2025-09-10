package main

import (
	"golang.org/x/exp/slices"
	"strings"
	"testing"
)

var services = `https://stackoverflow.com
https://www.google.com
https://go.dev
https://www.docker.com
https://kubernetes.io
https://www.finconsgroup.com
`

func TestHealthCheck(t *testing.T) {
	panic("TODO implements me")
}

func TestGetServices(t *testing.T) {
	want := []string{
		"https://stackoverflow.com",
		"https://www.google.com",
		"https://go.dev",
		"https://www.docker.com",
		"https://kubernetes.io",
		"https://www.finconsgroup.com",
	}

	got := GetServices(strings.NewReader(services))
	if slices.Compare(want, got) != 0 {
		t.Errorf("want: %v; got: %v", want, got)
	}
}
