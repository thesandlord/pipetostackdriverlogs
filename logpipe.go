// 	Copyright 2017, Google, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Command logpipe is a service that will let you pipe logs directly to Stackdriver Logging.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	flags "github.com/jessevdk/go-flags"

	"cloud.google.com/go/logging"
	"golang.org/x/net/context"
)

func main() {
	var opts struct {
		ProjectID string `short:"p" long:"project" description:"Google Cloud Platform Project ID" required:"true"`
		LogName   string `short:"l" long:"logname" description:"The name of the log to write to" default:"default"`
	}
	_, err := flags.Parse(&opts)
	if err != nil {
		os.Exit(2)
	}

	// Check if Standard In is coming from a pipe
	fi, err := os.Stdin.Stat()
	if err != nil {
		errorf("Could not stat standard input: %v", err)
	}
	if fi.Mode()&os.ModeNamedPipe == 0 {
		errorf("Nothing is piped in so there is nothing to log!")
	}

	// Creates a client.
	ctx := context.Background()
	client, err := logging.NewClient(ctx, opts.ProjectID)
	if err != nil {
		errorf("Failed to create client: %v", err)
	}
	errc := make(chan error)
	client.OnError = func(err error) { errc <- err }

	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		errorf("Failed to ping logging service: %v", err)
	}

	// Selects the log to write to.
	logger := client.Logger(opts.LogName)

	lines := make(chan string)
	go func() {
		defer close(lines)
		// Read from Stdin and log it to Stdout and Stackdriver
		s := bufio.NewScanner(io.TeeReader(os.Stdin, os.Stdout))
		for s.Scan() {
			lines <- s.Text()
		}
		if err := s.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to scan input: %v\n", err)
		}
	}()

	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)

loop:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break loop
			}
			logger.Log(logging.Entry{Payload: line})
		case s := <-signals:
			fmt.Fprintf(os.Stderr, "Terminating program after receiving signal: %v\n", s)
			break loop
		}
	}

	// Closes the client and flushes the buffer to the Stackdriver Logging
	// service.
	if err := client.Close(); err != nil {
		errorf("Failed to close client: %v", err)
	}
}

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
