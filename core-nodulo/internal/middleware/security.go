package middleware

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ValidateFrontendOrigin rejects wildcard, path-bearing, and non-HTTP origins.
func ValidateFrontendOrigin(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || raw == "*" {
		return fmt.Errorf("FRONTEND_ORIGIN deve ser uma única origem HTTP(S), sem caminho, consulta ou wildcard")
	}
	return nil
}

// SecurityHeaders adds browser protections appropriate for a JSON/SSE API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// CORS allows only the configured web origin and validates preflight methods
// and headers. Requests without an Origin header remain available to CLI and
// server-to-server clients, which still need normal authentication.
func CORS(frontendOrigin string, next http.Handler) http.Handler {
	allowedMethods := map[string]bool{
		http.MethodGet: true, http.MethodPost: true,
		http.MethodPatch: true, http.MethodDelete: true,
	}
	allowedHeaders := map[string]bool{"authorization": true, "content-type": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			if r.Method == http.MethodOptions {
				writeSecurityError(w, http.StatusForbidden, "Origem não autorizada.")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if origin != frontendOrigin {
			writeSecurityError(w, http.StatusForbidden, "Origem não autorizada.")
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", frontendOrigin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Registration-Message")
		if r.Method == http.MethodOptions {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			requestedMethod := strings.ToUpper(strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")))
			if !allowedMethods[requestedMethod] {
				writeSecurityError(w, http.StatusForbidden, "Método CORS não autorizado.")
				return
			}
			for _, requestedHeader := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				requestedHeader = strings.ToLower(strings.TrimSpace(requestedHeader))
				if requestedHeader != "" && !allowedHeaders[requestedHeader] {
					writeSecurityError(w, http.StatusForbidden, "Cabeçalho CORS não autorizado.")
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type authRatePolicy struct {
	limit  int
	window time.Duration
}

type rateWindow struct {
	started time.Time
	count   int
}

// AuthRateLimiter applies conservative per-source limits to public login and
// registration endpoints. Forwarded IPs are trusted only when the socket peer
// belongs to an explicitly configured trusted proxy CIDR.
func AuthRateLimiter(trustedProxies []*net.IPNet, next http.Handler) http.Handler {
	policies := map[string]authRatePolicy{
		"/api/auth/login":    {limit: 10, window: time.Minute},
		"/api/auth/register": {limit: 5, window: time.Minute},
	}
	var mu sync.Mutex
	windows := make(map[string]rateWindow)
	lastCleanup := time.Now()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policy, limitedRoute := policies[r.URL.Path]
		if !limitedRoute || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		key := r.URL.Path + "|" + requestClientIP(r, trustedProxies)
		now := time.Now()
		mu.Lock()
		if now.Sub(lastCleanup) >= time.Minute {
			for entryKey, entry := range windows {
				if now.Sub(entry.started) >= policyForPath(entryKey, policies).window {
					delete(windows, entryKey)
				}
			}
			lastCleanup = now
		}
		entry := windows[key]
		if entry.started.IsZero() || now.Sub(entry.started) >= policy.window {
			entry = rateWindow{started: now}
		}
		entry.count++
		windows[key] = entry
		limited := entry.count > policy.limit
		retryAfter := time.Duration(0)
		if limited {
			retryAfter = policy.window - now.Sub(entry.started)
		}
		mu.Unlock()

		if limited {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeSecurityError(w, http.StatusTooManyRequests, "Muitas tentativas em pouco tempo. Aguarde antes de tentar novamente.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ParseTrustedProxyCIDRs parses the optional TRUSTED_PROXY_CIDRS setting.
func ParseTrustedProxyCIDRs(raw string) ([]*net.IPNet, error) {
	entries := strings.Split(raw, ",")
	var networks []*net.IPNet
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS contém CIDR inválido %q: %w", entry, err)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func requestClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = r.RemoteAddr
	}
	peer := net.ParseIP(peerHost)
	if peer == nil {
		return "unknown"
	}
	if !isTrustedProxy(peer, trustedProxies) {
		return peer.String()
	}

	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	current := peer
	for index := len(forwarded) - 1; index >= 0 && isTrustedProxy(current, trustedProxies); index-- {
		candidate := net.ParseIP(strings.TrimSpace(forwarded[index]))
		if candidate == nil {
			return peer.String()
		}
		current = candidate
	}
	return current.String()
}

func isTrustedProxy(ip net.IP, trusted []*net.IPNet) bool {
	for _, network := range trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func policyForPath(key string, policies map[string]authRatePolicy) authRatePolicy {
	path, _, _ := strings.Cut(key, "|")
	if policy, ok := policies[path]; ok {
		return policy
	}
	return authRatePolicy{window: time.Minute}
}

func writeSecurityError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
