package configx

import (
	"context"
	"os"
	"strings"
	"time"

	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/spf13/cast"
	"google.golang.org/protobuf/types/known/durationpb"
)

func GetEnvOrString(key string, defaultValue string) string {
	if os.Getenv(key) != "" {
		return os.Getenv(key)
	}
	return defaultValue
}

func GetEnvOrStrings(key string, defaultValue []string) []string {
	if os.Getenv(key) != "" {
		if s := os.Getenv(key); s != "" {
			return strings.Split(s, ",")
		}
	}
	return defaultValue
}

func GetEnvOrInt64(key string, defaultValue int64) int64 {
	if os.Getenv(key) != "" {
		return cast.ToInt64(os.Getenv(key))
	}
	return defaultValue
}

func GetEnvOrInt(key string, defaultValue int) int {
	if os.Getenv(key) != "" {
		return cast.ToInt(os.Getenv(key))
	}
	return defaultValue
}

func GetEnvOrBool(key string, defaultValue bool) bool {
	if os.Getenv(key) != "" {
		return cast.ToBool(os.Getenv(key))
	}
	return defaultValue
}

func GetEnvOrDuration(key string, defaultValue *durationpb.Duration) time.Duration {
	if os.Getenv(key) != "" {
		d, err := time.ParseDuration(os.Getenv(key))
		if err == nil {
			return d
		}
	}
	if defaultValue != nil {
		return defaultValue.AsDuration()
	}
	return 0
}

func GetDomain(ctx context.Context) string {
	if req, ok := khttp.RequestFromServerContext(ctx); ok {
		// Get host from request (includes port if specified)
		host := req.Host
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], ".")
		}
		return host
	}
	return ""
}

func GetEnvOrStringsWithHostname(ctx context.Context, key string, defaultValue []string) []string {
	domain := strings.ToUpper(GetDomain(ctx))
	domain = strings.ReplaceAll(domain, ".", "_")
	return GetEnvOrStrings(domain+"_"+key, defaultValue)
}
