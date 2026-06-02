package testsupport

import (
	"net/url"
	"os"
	"strings"
)

// getenv is a tiny wrapper so the package has no other os dependency surface.
func getenv(key string) string { return os.Getenv(key) }

// swapDBName returns dsn with its database (URL path) replaced by name. It
// supports the URL form (postgres://user:pass@host:port/db?opts); for any other
// form it falls back to a best-effort suffix replace.
func swapDBName(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err == nil && u.Scheme != "" {
		u.Path = "/" + name
		return u.String()
	}
	// Fallback for keyword DSNs ("host=... dbname=...").
	if strings.Contains(dsn, "dbname=") {
		fields := strings.Fields(dsn)
		for i, f := range fields {
			if strings.HasPrefix(f, "dbname=") {
				fields[i] = "dbname=" + name
			}
		}
		return strings.Join(fields, " ")
	}
	return dsn
}
