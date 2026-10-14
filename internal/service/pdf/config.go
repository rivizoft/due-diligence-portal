package pdf

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

func LoadConfig() (Config, error) {
	c := Config{Workers: 2, QueueSize: 8, CacheBytes: 512 << 20, MaxInputBytes: 100 << 20, TTL: 15 * time.Minute, Timeout: 60 * time.Second, CacheDir: os.Getenv("DD_PDF_CACHE_DIR")}
	vals := []struct {
		name string
		p    *int64
	}{{"DD_PDF_CACHE_BYTES", &c.CacheBytes}, {"DD_PDF_MAX_INPUT_BYTES", &c.MaxInputBytes}}
	for _, v := range vals {
		if s, ok := os.LookupEnv(v.name); ok {
			n, e := strconv.ParseInt(s, 10, 64)
			if e != nil || n <= 0 {
				return c, fmt.Errorf("invalid %s", v.name)
			}
			*v.p = n
		}
	}
	for _, v := range []struct {
		name string
		p    *int
	}{{"DD_PDF_WORKERS", &c.Workers}, {"DD_PDF_QUEUE_SIZE", &c.QueueSize}} {
		if s, ok := os.LookupEnv(v.name); ok {
			n, e := strconv.Atoi(s)
			if e != nil || n <= 0 || n > 1024 {
				return c, fmt.Errorf("invalid %s", v.name)
			}
			*v.p = n
		}
	}
	for _, v := range []struct {
		name string
		p    *time.Duration
	}{{"DD_PDF_CACHE_TTL", &c.TTL}, {"DD_PDF_TIMEOUT", &c.Timeout}} {
		if s, ok := os.LookupEnv(v.name); ok {
			n, e := time.ParseDuration(s)
			if e != nil || n <= 0 {
				return c, fmt.Errorf("invalid %s", v.name)
			}
			*v.p = n
		}
	}
	return c, nil
}
