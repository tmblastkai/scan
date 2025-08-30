package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/security"
	"github.com/chromedp/chromedp"
)

// Process first performs a simple HTTP GET; if the response code is < 400, it
// proceeds to a full chromedp scan. Otherwise, the preflight status code is
// returned immediately.
func Process(rec InputRecord, timeout time.Duration) Result {
	url := fmt.Sprintf("%s://%s:%s", rec.Protocol, rec.Host, rec.Port)
	Warnf("preflight GET %s", url)

	client := newHTTPClient(timeout)
	res := Result{Raw: rec.Raw, Host: rec.Host, Port: rec.Port, Protocol: rec.Protocol, StartTime: time.Now().Format(time.RFC3339Nano)}
	resp, err := client.Get(url)
	if err != nil {
		res.Error = err.Error()
		Warnf("%s:%s preflight error: %v", rec.Host, rec.Port, err)
		return res
	}
	defer resp.Body.Close()
	res.ResponseCode = int64(resp.StatusCode)
	if resp.StatusCode >= 400 {
		Warnf("%s:%s preflight status %d - skipping deep scan", rec.Host, rec.Port, resp.StatusCode)
		return res
	}
	Warnf("%s:%s preflight status %d - proceeding with browser scan", rec.Host, rec.Port, resp.StatusCode)
	return chromedpProcess(res, timeout)
}

// chromedpProcess launches a headless browser to fetch information for a single host and port.
func chromedpProcess(res Result, timeout time.Duration) Result {
	url := fmt.Sprintf("%s://%s:%s", res.Protocol, res.Host, res.Port)
	Warnf("navigate to %s", url)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("ignore-certificate-errors", true),
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
			Warnf("%s:%s request %s", res.Host, res.Port, ev.Request.URL)
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			delete(requests, ev.RequestID)
			if len(requests) == 0 {
				idleTimer.Reset(quiet)
				Warnf("%s:%s network idle timer started", res.Host, res.Port)
			}
			Warnf("%s:%s request finished id=%s (active=%d)", res.Host, res.Port, ev.RequestID, len(requests))
			mu.Unlock()
		case *network.EventLoadingFailed:
			mu.Lock()
			delete(requests, ev.RequestID)
			if len(requests) == 0 {
				idleTimer.Reset(quiet)
				Warnf("%s:%s network idle timer started", res.Host, res.Port)
			}
			Warnf("%s:%s request failed id=%s (active=%d)", res.Host, res.Port, ev.RequestID, len(requests))
			mu.Unlock()
		case *network.EventResponseReceived:
			if ev.Type == network.ResourceTypeDocument && responseCode == 0 {
				responseCode = int64(ev.Response.Status)
				Warnf("%s:%s response %d for %s", res.Host, res.Port, responseCode, ev.Response.URL)
			}
		case *security.EventCertificateError:
			Warnf("%s:%s certificate error %s", res.Host, res.Port, ev.ErrorType)
			go func(id security.RequestID) {
				_ = chromedp.Run(ctx, security.HandleCertificateError(ev.EventID, security.CertificateErrorActionContinue))
			}(ev.RequestID)
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
						Warnf("%s:%s pruning lingering request %s", res.Host, res.Port, id)
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
	if err := chromedp.Run(ctx,
		network.Enable(),
		security.Enable(),
		security.SetIgnoreCertificateErrors(true),
		chromedp.Navigate(url),
	); err != nil {
		res.Error = err.Error()
		Warnf("navigate error %s:%s: %v", res.Host, res.Port, err)
		close(stopCh)
		return res
	}

	select {
	case <-idleCh:
		Warnf("%s:%s network idle", res.Host, res.Port)
	case <-timeoutTimer.C:
		mu.Lock()
		lingering := make([]string, 0, len(requests))
		for id := range requests {
			lingering = append(lingering, string(id))
		}
		mu.Unlock()
		Warnf("%s:%s context timeout: lingering requests %v", res.Host, res.Port, lingering)
	}
	timeoutTimer.Stop()
	close(stopCh)

	if err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
		chromedp.Title(&title),
		chromedp.Location(&finalURL),
	); err != nil {
		res.Error = err.Error()
		Warnf("%s:%s capture error: %v", res.Host, res.Port, err)
		return res
	}

	res.ResponseCode = responseCode
	res.HTMLHeader = title
	res.HasLoginKeyword = strings.Contains(strings.ToLower(html), "type=\"password\"")
	for _, u := range AllowList {
		if finalURL == u {
			res.IsMatched = true
			break
		}
	}
	if res.ResponseCode <= 400 {
		res.PassTest = true
	}
	if res.HasLoginKeyword || res.IsMatched {
		res.PassTest = true
	}
	Warnf("%s:%s finalURL=%s title=%q html=%dB", res.Host, res.Port, finalURL, title, len(html))
	Warnf("%s:%s result code=%d matched=%t login=%t pass=%t", res.Host, res.Port, res.ResponseCode, res.IsMatched, res.HasLoginKeyword, res.PassTest)
	return res
}
