package service

// URLSchemes contains URL schemes for browser & Arango Shell.
type URLSchemes struct {
	Browser  string // Scheme for use in a webbrowser
	ArangoSH string // URL Scheme for use in ArangoSH
}

// NewURLSchemes creates initialized schemes depending on isSecure flag.
func NewURLSchemes(isSecure bool) URLSchemes {
	if isSecure {
		return URLSchemes{"https", "ssl"}
	}
	return URLSchemes{"http", "tcp"}
}
