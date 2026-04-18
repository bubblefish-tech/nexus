// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

//go:build windows

package transport

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// WSLTransport connects to a WSL2 agent via localhost forwarding.
// On Windows, WSL2 automatically forwards ports from the Linux guest to
// the Windows host on localhost, so this transport rewrites the URL to
// use 127.0.0.1 and delegates to HTTP.
type WSLTransport struct{}

func newWSLTransport() (Transport, error) {
	return &WSLTransport{}, nil
}

// Dial opens a connection to a WSL2 agent via localhost forwarding.
func (t *WSLTransport) Dial(ctx context.Context, config TransportConfig) (Conn, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	rewritten, err := rewriteWSLURL(config.URL)
	if err != nil {
		return nil, err
	}
	config.URL = rewritten

	httpT := &HTTPTransport{}
	return httpT.Dial(ctx, config)
}

// Listen starts an HTTP listener for WSL connections.
func (t *WSLTransport) Listen(ctx context.Context, config TransportConfig) (Listener, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	rewritten, err := rewriteWSLURL(config.URL)
	if err != nil {
		return nil, err
	}
	config.URL = rewritten

	httpT := &HTTPTransport{}
	return httpT.Listen(ctx, config)
}

// rewriteWSLURL rewrites a URL to use 127.0.0.1, preserving the port.
func rewriteWSLURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("transport: wsl: parse url: %w", err)
	}

	host := u.Hostname()
	port := u.Port()

	// Rewrite to localhost if it looks like a WSL address.
	if host != "127.0.0.1" && host != "localhost" {
		host = "127.0.0.1"
	}

	if port == "" {
		return "", fmt.Errorf("transport: wsl: url must include a port")
	}

	u.Host = host + ":" + port

	result := u.String()
	// For Listen, we need just host:port, not full URL.
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return result, nil
	}
	return host + ":" + port, nil
}
