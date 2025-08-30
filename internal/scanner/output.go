package scanner

import (
	"encoding/csv"
	"fmt"
	"os"
)

// WriteResults appends results to the output CSV file.
func WriteResults(outputFile string, header []string, ch <-chan Result) {
	Warnf("writing results to %s", outputFile)
	_, err := os.Stat(outputFile)
	newFile := os.IsNotExist(err)

	f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		Warnf("open output error: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if newFile {
		Warnf("creating new output file with header")
		hdr := append(append([]string{}, header...),
			"protocol", "start_time", "response_code", "html_header", "has_login_keyword", "is_matched", "pass_test", "error")
		if err := w.Write(hdr); err != nil {
			Warnf("write header error: %v", err)
		}
		w.Flush()
	}

	count := 0
	for r := range ch {
		row := append(append([]string{}, r.Raw...),
			r.Protocol,
			r.StartTime,
			fmt.Sprintf("%d", r.ResponseCode),
			r.HTMLHeader,
			fmt.Sprintf("%t", r.HasLoginKeyword),
			fmt.Sprintf("%t", r.IsMatched),
			fmt.Sprintf("%t", r.PassTest),
			r.Error,
		)
		if err := w.Write(row); err != nil {
			Warnf("write row error for %s:%s: %v", r.Host, r.Port, err)
		}
		w.Flush()
		if err := w.Error(); err != nil {
			Warnf("flush error: %v", err)
		} else {
			Warnf("wrote result for %s:%s (%s)", r.Host, r.Port, r.Protocol)
		}
		count++
	}
	Infof("finished writing %d results", count)
}
