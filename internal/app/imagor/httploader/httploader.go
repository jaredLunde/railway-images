package httploader

import (
	"compress/gzip"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/cshum/imagor"
)

func randomProxyFunc(proxyURLs, hosts string) func(*http.Request) (*url.URL, error) {
	var urls []*url.URL
	var allowedSources []AllowedSource
	for _, split := range strings.Split(proxyURLs, ",") {
		if u, err := url.Parse(strings.TrimSpace(split)); err == nil {
			urls = append(urls, u)
		}
	}
	ln := len(urls)
	for _, host := range strings.Split(hosts, ",") {
		host = strings.TrimSpace(host)
		if len(host) > 0 {
			allowedSources = append(allowedSources, NewHostPatternAllowedSource(host))
		}
	}
	return func(r *http.Request) (u *url.URL, err error) {
		if len(urls) == 0 {
			return
		}
		if !isURLAllowed(r.URL, allowedSources) {
			return
		}
		u = urls[rand.Intn(ln)]
		return
	}
}

func isURLAllowed(u *url.URL, allowedSources []AllowedSource) bool {
	if !strings.Contains(u.Host, ".") && u.Host != "localhost" {
		return false
	}
	if len(allowedSources) == 0 {
		return true
	}
	_, err := net.LookupHost(u.Host)
	if err != nil {
		return false
	}
	for _, source := range allowedSources {
		if source.Match(u) {
			return true
		}
	}
	return false
}

func parseContentType(contentType string) string {
	idx := strings.Index(contentType, ";")
	if idx == -1 {
		idx = len(contentType)
	}
	return strings.TrimSpace(strings.ToLower(contentType[0:idx]))
}

func validateContentType(contentType string, accepts []string) bool {
	if len(accepts) == 0 {
		return true
	}
	contentType = parseContentType(contentType)
	for _, accept := range accepts {
		if ok, err := path.Match(accept, contentType); ok && err == nil {
			return true
		}
	}
	return false
}

// AllowedSource represents a source the HTTPLoader is allowed to load from.
// It supports host glob patterns such as *.google.com and a full URL regex.
type AllowedSource struct {
	HostPattern string
	URLRegex    *regexp.Regexp
}

// NewRegexpAllowedSource creates a new AllowedSource from the regex pattern
func NewRegexpAllowedSource(pattern string) (AllowedSource, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return AllowedSource{}, err
	}
	return AllowedSource{
		URLRegex: regex,
	}, nil
}

// NewHostPatternAllowedSource creates a new AllowedSource from the host glob pattern
func NewHostPatternAllowedSource(pattern string) AllowedSource {
	return AllowedSource{
		HostPattern: pattern,
	}
}

// Match checks if the url matches the AllowedSource
func (s AllowedSource) Match(u *url.URL) bool {
	if s.URLRegex != nil {
		return s.URLRegex.MatchString(u.String())
	}
	matched, e := path.Match(s.HostPattern, u.Host)
	return matched && e == nil
}

// HTTPLoader HTTP Loader implements imagor.Loader interface
type HTTPLoader struct {
	// The Transport used to request images, default http.DefaultTransport.
	Transport http.RoundTripper

	// ForwardHeaders copy request headers to image request headers
	ForwardHeaders []string

	// OverrideHeaders override image request headers
	OverrideHeaders map[string]string

	// OverrideResponseHeaders override image response header from HTTP Loader response
	OverrideResponseHeaders []string

	// AllowedSources list of sources allowed to load from
	AllowedSources []AllowedSource

	// Accept set request Accept and validate response Content-Type header
	Accept string

	// MaxAllowedSize maximum bytes allowed for image
	MaxAllowedSize int

	// DefaultScheme default image URL scheme
	DefaultScheme string

	// UserAgent default user agent for image request.
	// Can be overridden by ForwardHeaders and OverrideHeaders
	UserAgent string

	// BlockLoopbackNetworks rejects HTTP connections to loopback network IP addresses.
	BlockLoopbackNetworks bool

	// BlockPrivateNetworks rejects HTTP connections to private network IP addresses.
	BlockPrivateNetworks bool

	// BlockLinkLocalNetworks rejects HTTP connections to link local IP addresses.
	BlockLinkLocalNetworks bool

	// BlockNetworks rejects HTTP connections to a configurable list of networks.
	BlockNetworks []*net.IPNet

	// BaseURL base URL for HTTP loader
	BaseURL *url.URL

	accepts []string
}

// New creates HTTPLoader
func New(options ...Option) *HTTPLoader {
	h := &HTTPLoader{
		OverrideHeaders: map[string]string{},
		DefaultScheme:   "https",
		Accept:          "*/*",
		UserAgent:       fmt.Sprintf("imagor/%s", imagor.Version),
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Control: h.DialControl}
	transport.DialContext = dialer.DialContext
	h.Transport = transport

	for _, option := range options {
		option(h)
	}
	if s := strings.ToLower(h.DefaultScheme); s == "nil" {
		h.DefaultScheme = ""
	}
	if h.Accept != "" {
		for _, seg := range strings.Split(h.Accept, ",") {
			if typ := parseContentType(seg); typ != "" {
				h.accepts = append(h.accepts, typ)
			}
		}
	}
	return h
}

// Get implements imagor.Loader interface
func (h *HTTPLoader) Get(r *http.Request, image string) (*imagor.Blob, error) {
	if strings.HasPrefix(image, "files/") {
		return nil, imagor.ErrNotFound
	}
	if image == "" {
		return nil, imagor.ErrInvalid
	}
	u, err := url.Parse(image)
	if err != nil {
		return nil, imagor.ErrInvalid
	}
	if h.BaseURL != nil {
		newU := h.BaseURL.JoinPath(u.Path)
		newU.RawQuery = u.RawQuery
		image = newU.String()
		u = newU
	}
	if u.Host == "" || u.Scheme == "" {
		if h.DefaultScheme != "" {
			image = h.DefaultScheme + "://" + image
			if u, err = url.Parse(image); err != nil {
				return nil, imagor.ErrInvalid
			}
		} else {
			return nil, imagor.ErrInvalid
		}
	}

	// Basic cleanup of the URL by dropping the fragment and cleaning up the
	// path which is important for matching against allowed sources.
	u = u.JoinPath()
	u.Fragment = ""

	if !isURLAllowed(u, h.AllowedSources) {
		return nil, imagor.ErrSourceNotAllowed
	}
	client := &http.Client{
		Transport:     h.Transport,
		CheckRedirect: h.checkRedirect,
	}
	if h.MaxAllowedSize > 0 {
		req, err := h.newRequest(r, http.MethodHead, image)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 && resp.StatusCode > 206 {
			return nil, imagor.NewErrorFromStatusCode(resp.StatusCode)
		}
		contentLength, _ := strconv.Atoi(resp.Header.Get("Content-Length"))
		if contentLength > h.MaxAllowedSize {
			return nil, imagor.ErrMaxSizeExceeded
		}
	}
	req, err := h.newRequest(r, http.MethodGet, image)
	if err != nil {
		return nil, err
	}
	var blob *imagor.Blob
	var once sync.Once
	blob = imagor.NewBlob(func() (io.ReadCloser, int64, error) {
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, ErrUnauthorizedRequest) {
				err = imagor.NewError(
					fmt.Sprintf("%s: %s", err.Error(), image),
					http.StatusForbidden)
			} else if idx := strings.Index(err.Error(), "dial tcp: "); idx > -1 {
				err = imagor.NewError(
					fmt.Sprintf("%s: %s", err.Error()[idx:], image),
					http.StatusNotFound)
			}
			return nil, 0, err
		}
		once.Do(func() {
			blob.SetContentType(resp.Header.Get("Content-Type"))
			if len(h.OverrideResponseHeaders) > 0 {
				blob.Header = make(http.Header)
				for _, key := range h.OverrideResponseHeaders {
					if val := resp.Header.Get(key); val != "" {
						blob.Header.Set(key, val)
					}
				}
			}
		})
		body := resp.Body
		size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzipBody, err := gzip.NewReader(resp.Body)
			if err != nil {
				return nil, 0, err
			}
			body = gzipBody
			size = 0 // size unknown after decompress
		}
		if resp.StatusCode >= 400 {
			return body, size, imagor.NewErrorFromStatusCode(resp.StatusCode)
		}
		if !validateContentType(resp.Header.Get("Content-Type"), h.accepts) {
			return body, size, imagor.ErrUnsupportedFormat
		}
		return body, size, nil
	})
	return blob, nil
}

func (h *HTTPLoader) newRequest(r *http.Request, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(r.Context(), method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", h.UserAgent)
	if h.Accept != "" {
		req.Header.Set("Accept", h.Accept)
	}
	for _, header := range h.ForwardHeaders {
		if header == "*" {
			req.Header = r.Header.Clone()
			req.Header.Del("Accept-Encoding") // fix compressions
			break
		}
		if _, ok := r.Header[header]; ok {
			req.Header.Set(header, r.Header.Get(header))
		}
	}
	for key, value := range h.OverrideHeaders {
		req.Header.Set(key, value)
	}
	return req, nil
}

func (h *HTTPLoader) checkRedirect(r *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if !isURLAllowed(r.URL, h.AllowedSources) {
		return imagor.ErrSourceNotAllowed
	}
	return nil
}

// ErrUnauthorizedRequest unauthorized request error
var ErrUnauthorizedRequest = errors.New("unauthorized request")

// DialControl implements a net.Dialer.Control function which is automatically used with the default http.Transport.
// If the transport is replaced using the WithTransport option it is up to that
// transport if the control function is used or not.
func (h *HTTPLoader) DialControl(network string, address string, conn syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr := net.ParseIP(host)
	if h.BlockLoopbackNetworks && addr.IsLoopback() {
		return ErrUnauthorizedRequest
	}
	if h.BlockLinkLocalNetworks && (addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()) {
		return ErrUnauthorizedRequest
	}
	if h.BlockPrivateNetworks && addr.IsPrivate() {
		return ErrUnauthorizedRequest
	}
	for _, network := range h.BlockNetworks {
		if network.Contains(addr) {
			return ErrUnauthorizedRequest
		}
	}
	return nil
}

// Option HTTPLoader option
type Option func(h *HTTPLoader)

// WithTransport with custom http.RoundTripper transport option
func WithTransport(transport http.RoundTripper) Option {
	return func(h *HTTPLoader) {
		if transport != nil {
			h.Transport = transport
		}
	}
}

// WithProxyTransport with random proxy rotation option for selected proxy URLs
func WithProxyTransport(proxyURLs, hosts string) Option {
	return func(h *HTTPLoader) {
		if proxyURLs != "" {
			if t, ok := h.Transport.(*http.Transport); ok {
				t.Proxy = randomProxyFunc(proxyURLs, hosts)
				h.Transport = t
			}
		}
	}
}

// WithInsecureSkipVerifyTransport with insecure HTTPs option
func WithInsecureSkipVerifyTransport(enabled bool) Option {
	return func(h *HTTPLoader) {
		if enabled {
			if t, ok := h.Transport.(*http.Transport); ok {
				t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
				h.Transport = t
			}
		}
	}
}

// WithForwardHeaders with forward selected request headers option
func WithForwardHeaders(headers ...string) Option {
	return func(h *HTTPLoader) {
		for _, raw := range headers {
			splits := strings.Split(raw, ",")
			for _, header := range splits {
				header = strings.TrimSpace(header)
				if len(header) > 0 {
					h.ForwardHeaders = append(h.ForwardHeaders, header)
				}
			}
		}
	}
}

// WithOverrideResponseHeaders with override selected response headers option
func WithOverrideResponseHeaders(headers ...string) Option {
	return func(h *HTTPLoader) {
		for _, raw := range headers {
			splits := strings.Split(raw, ",")
			for _, header := range splits {
				header = strings.TrimSpace(header)
				if len(header) > 0 {
					h.OverrideResponseHeaders = append(h.OverrideResponseHeaders, header)
				}
			}
		}
	}
}

// WithForwardClientHeaders with forward browser request headers option
func WithForwardClientHeaders(enabled bool) Option {
	return func(h *HTTPLoader) {
		if enabled {
			h.ForwardHeaders = []string{"*"}
		}
	}
}

// WithOverrideHeader with override request header with name value pair option
func WithOverrideHeader(name, value string) Option {
	return func(h *HTTPLoader) {
		h.OverrideHeaders[name] = value
	}
}

// WithAllowedSources with allowed source hosts option.
// Accept csv wth glob pattern e.g. *.google.com,*.github.com
func WithAllowedSources(hosts ...string) Option {
	return func(h *HTTPLoader) {
		for _, raw := range hosts {
			splits := strings.Split(raw, ",")
			for _, host := range splits {
				host = strings.TrimSpace(host)
				if len(host) > 0 {
					h.AllowedSources = append(h.AllowedSources,
						NewHostPatternAllowedSource(host))
				}
			}
		}
	}
}

func WithAllowedSourceRegexps(patterns ...string) Option {
	return func(h *HTTPLoader) {
		for _, pat := range patterns {
			if as, err := NewRegexpAllowedSource(pat); pat != "" && err == nil {
				h.AllowedSources = append(h.AllowedSources, as)
			}
		}
	}
}

// WithMaxAllowedSize with maximum allowed size option
func WithMaxAllowedSize(maxAllowedSize int) Option {
	return func(h *HTTPLoader) {
		if maxAllowedSize > 0 {
			h.MaxAllowedSize = maxAllowedSize
		}
	}
}

// WithUserAgent with custom user agent option
func WithUserAgent(userAgent string) Option {
	return func(h *HTTPLoader) {
		if userAgent != "" {
			h.UserAgent = userAgent
		}
	}
}

// WithAccept with accepted content type option
func WithAccept(contentType string) Option {
	return func(h *HTTPLoader) {
		if contentType != "" {
			h.Accept = contentType
		}
	}
}

// WithDefaultScheme with default URL scheme option https or http, if not specified
func WithDefaultScheme(scheme string) Option {
	return func(h *HTTPLoader) {
		if scheme != "" {
			h.DefaultScheme = scheme
		}
	}
}

// WithBaseURL with base URL option for valid URL string
func WithBaseURL(baseURL string) Option {
	return func(h *HTTPLoader) {
		if baseURL != "" {
			if u, err := url.Parse(baseURL); err == nil {
				h.BaseURL = u
			}
		}
	}
}

// WithBlockLoopbackNetworks with option to reject HTTP connections
// to loopback network IP addresses
func WithBlockLoopbackNetworks(enabled bool) Option {
	return func(h *HTTPLoader) {
		if enabled {
			h.BlockLoopbackNetworks = true
		}
	}
}

// WithBlockLinkLocalNetworks with option to reject HTTP connections
// to link local IP addresses
func WithBlockLinkLocalNetworks(enabled bool) Option {
	return func(h *HTTPLoader) {
		if enabled {
			h.BlockLinkLocalNetworks = true
		}
	}
}

// WithBlockPrivateNetworks with option to reject HTTP connections
// to private network IP addresses
func WithBlockPrivateNetworks(enabled bool) Option {
	return func(h *HTTPLoader) {
		if enabled {
			h.BlockPrivateNetworks = true
		}
	}
}

// WithBlockNetworks with option to reject
// HTTP connections to a configurable list of networks
func WithBlockNetworks(networks ...*net.IPNet) Option {
	return func(h *HTTPLoader) {
		h.BlockNetworks = networks
	}
}
