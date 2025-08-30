package scanner

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
)

// ReadInput reads the input CSV and filters out records already present in the output CSV.
func ReadInput(inputFile, outputFile string) ([]InputRecord, []string, error) {
	Warnf("reading input file %s", inputFile)
	f, err := os.Open(inputFile)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("input file is empty")
	}

	header := rows[0]
	hostIdx, portIdx := -1, -1
	for i, h := range header {
		switch strings.ToLower(h) {
		case "host":
			hostIdx = i
		case "port":
			portIdx = i
		}
	}
	if hostIdx < 0 || portIdx < 0 {
		return nil, nil, fmt.Errorf("host or port column not found")
	}

	processed := make(map[string]struct{})
	if of, err := os.Open(outputFile); err == nil {
		Warnf("checking existing results in %s", outputFile)
		defer of.Close()
		or := csv.NewReader(of)
		outRows, err := or.ReadAll()
		if err == nil && len(outRows) > 0 {
			outHeader := outRows[0]
			hIdx, pIdx, protoIdx := -1, -1, -1
			for i, h := range outHeader {
				switch strings.ToLower(h) {
				case "host":
					hIdx = i
				case "port":
					pIdx = i
				case "protocol":
					protoIdx = i
				}
			}
			for i, row := range outRows {
				if i == 0 {
					continue
				}
				if hIdx < 0 || pIdx < 0 || protoIdx < 0 {
					continue
				}
				if len(row) <= protoIdx {
					continue
				}
				key := row[hIdx] + ":" + row[pIdx] + ":" + row[protoIdx]
				processed[key] = struct{}{}
			}
			Infof("found %d processed records", len(processed))
		} else if err != nil {
			Warnf("read output error: %v", err)
		}
	}

	var result []InputRecord
	for i, row := range rows {
		if i == 0 {
			continue
		}
		if len(row) <= portIdx || len(row) <= hostIdx {
			continue
		}
		host, port := row[hostIdx], row[portIdx]
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
			raw := make([]string, len(row))
			copy(raw, row)
			result = append(result, InputRecord{Host: host, Port: port, Protocol: proto, Raw: raw})
		}
	}
	Infof("parsed %d new records", len(result))
	return result, header, nil
}
