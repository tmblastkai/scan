package main

import (
	"flag"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"scan/internal/scanner"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	inputPath := flag.String("file", "input.csv", "input CSV file containing host and port columns")
	outputPath := flag.String("output", "output.csv", "output CSV file path")
	concurrency := flag.Int("concurrency", 16, "maximum concurrent chromedp tabs")
	timeoutSec := flag.Int("timeout", 5, "chromedp timeout in seconds")
	logPath := flag.String("log", "scan.log", "log file path")
	levelFlag := flag.String("log-level", "info", "log level (info or warn)")
	flag.Parse()

	switch strings.ToLower(*levelFlag) {
	case "warn", "warning":
		scanner.SetLogLevel(scanner.LevelWarn)
	default:
		scanner.SetLogLevel(scanner.LevelInfo)
	}

	lf, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("open log file %s: %v", *logPath, err)
	}
	defer lf.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, lf))

	scanner.Warnf("flags: file=%s output=%s concurrency=%d timeout=%d log=%s", *inputPath, *outputPath, *concurrency, *timeoutSec, *logPath)
	inputRecords, header, err := scanner.ReadInput(*inputPath, *outputPath)
	if err != nil {
		scanner.Warnf("read input error: %v", err)
		return
	}
	scanner.Infof("input_records: %d", len(inputRecords))
	if len(inputRecords) == 0 {
		scanner.Infof("no input records to process, exiting")
		return
	}

	outCh := make(chan scanner.Result)
	var (
		wg       sync.WaitGroup
		writerWg sync.WaitGroup
	)

	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		scanner.WriteResults(*outputPath, header, outCh)
	}()

	var remaining int64 = int64(len(inputRecords))
	ticker := time.NewTicker(time.Minute)
	go func() {
		for range ticker.C {
			rem := atomic.LoadInt64(&remaining)
			scanner.Infof("tasks remaining: %d", rem)
			if rem <= 0 {
				ticker.Stop()
				return
			}
		}
	}()

	sem := make(chan struct{}, *concurrency)
	for _, rec := range inputRecords {
		wg.Add(1)
		sem <- struct{}{}
		go func(r scanner.InputRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			scanner.Warnf("start processing %s:%s (%s)", r.Host, r.Port, r.Protocol)
			res := scanner.Process(r, time.Duration(*timeoutSec)*time.Second)
			outCh <- res
			atomic.AddInt64(&remaining, -1)
			scanner.Warnf("finish processing %s:%s (%s)", r.Host, r.Port, r.Protocol)
		}(rec)
	}

	wg.Wait()
	close(outCh)
	ticker.Stop()
	writerWg.Wait()
	scanner.Warnf("all tasks completed")
}
