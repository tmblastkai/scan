package scanner

// InputRecord represents a host, port, and protocol combination.
type InputRecord struct {
	Host     string
	Port     string
	Protocol string
	Raw      []string
}

// Result holds the scan outcome for a single host, port, and protocol.
type Result struct {
	Raw             []string
	Host            string
	Port            string
	Protocol        string
	StartTime       string
	ResponseCode    int64
	HTMLHeader      string
	HasLoginKeyword bool
	IsMatched       bool
	PassTest        bool
	Error           string
}

// AllowList contains URLs that are considered valid landing pages.
var AllowList = []string{}
