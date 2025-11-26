package main

import (
	"fmt"
	"io"
)

type prefixedWriter struct {
	prefix string
	wr     io.Writer
}

func (pw *prefixedWriter) Write(p []byte) (int, error) {
	return fmt.Fprintf(pw.wr, "%s | %s\n", pw.prefix, p)
}
