/*
MIT License

Copyright (c) 2023-2026 The Trzsz SSH Authors.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package tssh

import (
	"strings"
	"time"
)

var promptCursorIcon = "🧨"
var promptSelectedIcon = "🍺"

func matchHost(h *sshHost, keywords []string) bool {
	host := strings.ToLower(h.Host)
	alias := strings.ToLower(h.Alias)
	labels := strings.ToLower(h.GroupLabels)
	for _, keyword := range keywords {
		if !strings.Contains(host, keyword) &&
			!strings.Contains(alias, keyword) &&
			!strings.Contains(labels, keyword) {
			return false
		}
	}
	return true
}

func predictDestination(dest string) (string, bool, error) {
	if !isTerminal || strings.ContainsAny(dest, ".:[]@") {
		return dest, false, nil
	}

	if userConfig.useOpenSSHConfig {
		return dest, false, nil
	}

	hosts := getAllHosts()
	for _, host := range hosts {
		if host.Alias == dest {
			return dest, false, nil
		}
	}

	for _, pattern := range userConfig.wildcardPatterns {
		if pattern.Regex().MatchString(dest) {
			return dest, false, nil
		}
	}

	match := false
	keywords := strings.Fields(strings.ToLower(dest))
	for _, host := range hosts {
		if matchHost(host, keywords) {
			match = true
			break
		}
	}
	if !match {
		return dest, false, nil
	}

	if _, err := lookupHostWithTimeout(dest, 200*time.Millisecond); err == nil {
		return dest, false, nil
	}

	return chooseAlias(dest)
}
