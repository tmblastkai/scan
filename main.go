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

// InputRecord represents a host and port combination.
type InputRecord struct {
	Host string
	Port string
}

// Result holds the scan outcome for a single host and port.
type Result struct {
	Host            string
	Port            string
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
	var wg sync.WaitGroup

	go func() {
		writeResults(*outputPath, outCh)
	}()

	sem := make(chan struct{}, *concurrency)
	for _, rec := range inputRecords {
		wg.Add(1)
		sem <- struct{}{}
		go func(r InputRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			log.Printf("start processing %s:%s", r.Host, r.Port)
			res := process(r, time.Duration(*timeoutSec)*time.Second)
			outCh <- res
			log.Printf("finish processing %s:%s", r.Host, r.Port)
		}(rec)
	}

	wg.Wait()
	close(outCh)
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
				if i == 0 || len(row) < 2 {
					continue
				}
				key := row[0] + ":" + row[1]
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
		key := row[0] + ":" + row[1]
		if _, ok := processed[key]; ok {
			continue
		}
		result = append(result, InputRecord{Host: row[0], Port: row[1]})
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
		if err := w.Write([]string{"host", "port", "response_code", "html_header", "has_login_keyword", "is_matched", "pass_test", "error"}); err != nil {
			log.Printf("write header error: %v", err)
		}
		w.Flush()
	}

	count := 0
	for r := range ch {
		row := []string{
			r.Host,
			r.Port,
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
			log.Printf("wrote result for %s:%s", r.Host, r.Port)
		}
		count++
	}
	log.Printf("finished writing %d results", count)
}

// process launches a headless browser to fetch information for a single host and port.
func process(rec InputRecord, timeout time.Duration) Result {
	res := Result{Host: rec.Host, Port: rec.Port}
	url := fmt.Sprintf("http://%s:%s", rec.Host, rec.Port)
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

	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	quiet := 500 * time.Millisecond
	var (
		mu        sync.Mutex
		active    int
		html      string
		title     string
		finalURL  string
		idleTimer = time.NewTimer(time.Hour)
		done      = make(chan struct{})
	)
	idleTimer.Stop()
	var responseCode int64

	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			mu.Lock()
			active++
			idleTimer.Stop()
			log.Printf("%s:%s request %s", rec.Host, rec.Port, ev.Request.URL)
			mu.Unlock()
		case *network.EventLoadingFinished, *network.EventLoadingFailed:
			mu.Lock()
			if active > 0 {
				active--
			}
			if active == 0 {
				idleTimer.Reset(quiet)
				log.Printf("%s:%s network idle timer started", rec.Host, rec.Port)
			}
			log.Printf("%s:%s request finished (active=%d)", rec.Host, rec.Port, active)
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
		close(done)
	}()

	if err := chromedp.Run(ctx, network.Enable(), chromedp.Navigate(url)); err != nil {
		res.Error = err.Error()
		log.Printf("navigate error %s:%s: %v", rec.Host, rec.Port, err)
		return res
	}

	select {
	case <-done:
		log.Printf("%s:%s network idle", rec.Host, rec.Port)
	case <-ctx.Done():
		res.Error = ctx.Err().Error()
		log.Printf("%s:%s context timeout: %v", rec.Host, rec.Port, ctx.Err())
		return res
	}

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
