package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestMain(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	PrintHello()
	got := buf.String()
	if !strings.Contains(got, "Hello, World!") {
		t.Errorf("PrintHello() = %s; want 'Hello, World!'", got)
	}
}
