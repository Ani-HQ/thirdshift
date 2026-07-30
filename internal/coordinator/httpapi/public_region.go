package httpapi

import (
	"net/http"
	"strings"
)

func (o Options) requesterRegion(r *http.Request) *string {
	headers := []string{o.RequesterRegionHeader, "CF-IPCountry"}
	seen := map[string]bool{}
	for _, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" || seen[strings.ToLower(header)] {
			continue
		}
		seen[strings.ToLower(header)] = true
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if len(value) > 64 {
			value = value[:64]
		}
		return &value
	}
	return nil
}
