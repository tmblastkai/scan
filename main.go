package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// InputRecord represents a host, port, and protocol combination.
type InputRecord struct {
	Host     string
	Port     string
	Protocol string
}

// Result holds the scan outcome for a single host, port, and protocol.
type Result struct {
	Host            string
	Port            string
	Protocol        string
	ResponseCode    int64
	HTMLHeader      string
	HasLoginKeyword bool
	IsMatched       bool
	PassTest        bool
	Error           string
}

// allowList contains URLs that are considered valid landing pages.
var allowList = []string{
	// add allowed URLs here
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	inputPath := flag.String("file", "input.csv", "input CSV file containing host and port columns")
	outputPath := flag.String("output", "output.csv", "output CSV file path")
	concurrency := flag.Int("concurrency", 16, "maximum concurrent chromedp tabs")
	timeoutSec := flag.Int("timeout", 5, "chromedp timeout in seconds")
	flag.Parse()

	log.Printf("flags: file=%s output=%s concurrency=%d timeout=%d", *inputPath, *outputPath, *concurrency, *timeoutSec)
	inputRecords, err := readInput(*inputPath, *outputPath)
	if err != nil {
		log.Printf("read input error: %v", err)
		return
	}
	log.Printf("input_records: %d", len(inputRecords))
	if len(inputRecords) == 0 {
		log.Printf("no input records to process, exiting")
		return
	}

	outCh := make(chan Result)
	var (
		wg       sync.WaitGroup
		writerWg sync.WaitGroup
	)

	writerWg.Add(1)
	go func() {
		defer writerWg.Done()
		writeResults(*outputPath, outCh)
	}()

	sem := make(chan struct{}, *concurrency)
	for _, rec := range inputRecords {
		wg.Add(1)
		sem <- struct{}{}
		go func(r InputRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			log.Printf("start processing %s:%s (%s)", r.Host, r.Port, r.Protocol)
			res := process(r, time.Duration(*timeoutSec)*time.Second)
			outCh <- res
			log.Printf("finish processing %s:%s (%s)", r.Host, r.Port, r.Protocol)
		}(rec)
	}

	wg.Wait()
	close(outCh)
	writerWg.Wait()
	log.Printf("all tasks completed")
}

// readInput reads the input CSV and filters out records already present in the output CSV.
func readInput(inputFile, outputFile string) ([]InputRecord, error) {
	log.Printf("reading input file %s", inputFile)
	f, err := os.Open(inputFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	processed := make(map[string]struct{})
	if of, err := os.Open(outputFile); err == nil {
		log.Printf("checking existing results in %s", outputFile)
		defer of.Close()
		or := csv.NewReader(of)
		outRows, err := or.ReadAll()
		if err == nil {
			for i, row := range outRows {
				if i == 0 || len(row) < 3 {
					continue
				}
				key := row[0] + ":" + row[1] + ":" + row[2]
				processed[key] = struct{}{}
			}
			log.Printf("found %d processed records", len(processed))
		} else {
			log.Printf("read output error: %v", err)
		}
	}

	var result []InputRecord
	for i, row := range rows {
		if i == 0 || len(row) < 2 {
			continue
		}
		host, port := row[0], row[1]
		var protos []string
		switch port {
		case "80":
			protos = []string{"http"}
		case "443":
			protos = []string{"https"}
		default:
			protos = []string{"http", "https"}
		}
		for _, proto := range protos {
			key := host + ":" + port + ":" + proto
			if _, ok := processed[key]; ok {
				continue
			}
			result = append(result, InputRecord{Host: host, Port: port, Protocol: proto})
		}
	}
	log.Printf("parsed %d new records", len(result))
	return result, nil
}

// writeResults appends results to the output CSV file.
func writeResults(outputFile string, ch <-chan Result) {
	log.Printf("writing results to %s", outputFile)
	_, err := os.Stat(outputFile)
	newFile := os.IsNotExist(err)

	f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("open output error: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if newFile {
		log.Printf("creating new output file with header")
		if err := w.Write([]string{"host", "port", "protocol", "response_code", "html_header", "has_login_keyword", "is_matched", "pass_test", "error"}); err != nil {
			log.Printf("write header error: %v", err)
		}
		w.Flush()
	}

	count := 0
	for r := range ch {
		row := []string{
			r.Host,
			r.Port,
			r.Protocol,
			fmt.Sprintf("%d", r.ResponseCode),
			r.HTMLHeader,
			fmt.Sprintf("%t", r.HasLoginKeyword),
			fmt.Sprintf("%t", r.IsMatched),
			fmt.Sprintf("%t", r.PassTest),
			r.Error,
		}
		if err := w.Write(row); err != nil {
			log.Printf("write row error for %s:%s: %v", r.Host, r.Port, err)
		}
		w.Flush()
		if err := w.Error(); err != nil {
			log.Printf("flush error: %v", err)
		} else {
			log.Printf("wrote result for %s:%s (%s)", r.Host, r.Port, r.Protocol)
		}
		count++
	}
	log.Printf("finished writing %d results", count)
}

// process launches a headless browser to fetch information for a single host and port.
func process(rec InputRecord, timeout time.Duration) Result {
	res := Result{Host: rec.Host, Port: rec.Port, Protocol: rec.Protocol}
	url := fmt.Sprintf("%s://%s:%s", rec.Protocol, rec.Host, rec.Port)
	log.Printf("navigate to %s", url)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.NoSandbox,
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	defer cancelCtx()

	quiet := 500 * time.Millisecond
	var (
		mu        sync.Mutex
		requests  = make(map[network.RequestID]time.Time)
		html      string
		title     string
		finalURL  string
		idleTimer = time.NewTimer(time.Hour)
		idleCh    = make(chan struct{}, 1)
		stopCh    = make(chan struct{})
	)
	idleTimer.Stop()
	var responseCode int64

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			mu.Lock()
			requests[ev.RequestID] = time.Now()
			idleTimer.Stop()
			log.Printf("%s:%s request %s", rec.Host, rec.Port, ev.Request.URL)
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			delete(requests, ev.RequestID)
			if len(requests) == 0 {
				idleTimer.Reset(quiet)
				log.Printf("%s:%s network idle timer started", rec.Host, rec.Port)
			}
			log.Printf("%s:%s request finished id=%s (active=%d)", rec.Host, rec.Port, ev.RequestID, len(requests))
			mu.Unlock()
		case *network.EventLoadingFailed:
			mu.Lock()
			delete(requests, ev.RequestID)
			if len(requests) == 0 {
				idleTimer.Reset(quiet)
				log.Printf("%s:%s network idle timer started", rec.Host, rec.Port)
			}
			log.Printf("%s:%s request failed id=%s (active=%d)", rec.Host, rec.Port, ev.RequestID, len(requests))
			mu.Unlock()
		case *network.EventResponseReceived:
			if ev.Type == network.ResourceTypeDocument && responseCode == 0 {
				responseCode = int64(ev.Response.Status)
				log.Printf("%s:%s response %d for %s", rec.Host, rec.Port, responseCode, ev.Response.URL)
			}
		}
	})

	go func() {
		<-idleTimer.C
		idleCh <- struct{}{}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	go func() {
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				now := time.Now()
				for id, start := range requests {
					if now.Sub(start) > quiet {
						log.Printf("%s:%s pruning lingering request %s", rec.Host, rec.Port, id)
						delete(requests, id)
					}
				}
				if len(requests) == 0 {
					idleTimer.Reset(quiet)
				}
				mu.Unlock()
			case <-stopCh:
				ticker.Stop()
				return
			}
		}
	}()

	timeoutTimer := time.NewTimer(timeout)
	if err := chromedp.Run(ctx, network.Enable(), chromedp.Navigate(url)); err != nil {
		res.Error = err.Error()
		log.Printf("navigate error %s:%s: %v", rec.Host, rec.Port, err)
		close(stopCh)
		return res
	}

	select {
	case <-idleCh:
		log.Printf("%s:%s network idle", rec.Host, rec.Port)
	case <-timeoutTimer.C:
		mu.Lock()
		lingering := make([]string, 0, len(requests))
		for id := range requests {
			lingering = append(lingering, string(id))
		}
		mu.Unlock()
		log.Printf("%s:%s context timeout: lingering requests %v", rec.Host, rec.Port, lingering)
	}
	timeoutTimer.Stop()
	close(stopCh)

	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
		chromedp.Title(&title),
		chromedp.Location(&finalURL),
	); err != nil {
		res.Error = err.Error()
		log.Printf("%s:%s capture error: %v", rec.Host, rec.Port, err)
		return res
	}

	res.ResponseCode = responseCode
	res.HTMLHeader = title
	res.HasLoginKeyword = strings.Contains(strings.ToLower(html), "type=\"password\"")
	for _, u := range allowList {
		if finalURL == u {
			res.IsMatched = true
			break
		}
	}
	res.PassTest = res.HasLoginKeyword || res.IsMatched
	log.Printf("%s:%s finalURL=%s title=%q html=%dB", rec.Host, rec.Port, finalURL, title, len(html))
	log.Printf("%s:%s result code=%d matched=%t login=%t pass=%t", rec.Host, rec.Port, res.ResponseCode, res.IsMatched, res.HasLoginKeyword, res.PassTest)
	return res
}
