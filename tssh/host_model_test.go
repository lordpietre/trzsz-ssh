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
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestHostModelSearchKeyHandling(t *testing.T) {
	assert := assert.New(t)
	userConfig = &tsshConfig{}

	hosts := []*sshHost{
		{Alias: "web-prod-1", Host: "1.1.1.1"},
		{Alias: "web-prod-2", Host: "1.1.1.2"},
		{Alias: "db-prod", Host: "2.2.2.2"},
		{Alias: "web-dev", Host: "3.3.3.3"},
	}

	// Create a host model starting in search mode
	m := newHostModel("/", hosts, nil)
	assert.True(m.search)
	assert.Equal("/", string(m.filter))
	assert.Equal(0, m.cursor)
	assert.Len(m.filtered, 4)

	// Helper to send KeyPressMsg
	sendKey := func(k tea.Key) {
		m.Update(tea.KeyPressMsg(k))
	}

	// 1. Type character 'w'
	sendKey(tea.Key{Text: "w"})
	assert.Equal("/w", string(m.filter))
	assert.True(m.search)
	assert.Len(m.filtered, 3) // web-prod-1, web-prod-2, web-dev
	assert.Equal(0, m.cursor)

	// 2. Type space ' '
	sendKey(tea.Key{Code: ' ', Text: " "})
	assert.Equal("/w ", string(m.filter))
	assert.True(m.search)
	assert.Len(m.filtered, 3)

	// 3. Type characters "dev"
	sendKey(tea.Key{Text: "d"})
	sendKey(tea.Key{Text: "e"})
	sendKey(tea.Key{Text: "v"})
	assert.Equal("/w dev", string(m.filter))
	assert.True(m.search)
	assert.Len(m.filtered, 1) // web-dev
	assert.Equal("web-dev", m.filtered[0].Alias)
	assert.Equal(0, m.cursor)

	// Clear the search by pressing esc
	sendKey(tea.Key{Code: tea.KeyEscape})
	assert.False(m.search)
	assert.Nil(m.filter)
	assert.Len(m.filtered, 4)

	// Go back to search mode
	m.search = true
	m.filter = []byte("/")
	m.applyFilter()

	// Type 'w' to get 3 hosts
	sendKey(tea.Key{Text: "w"})
	assert.Len(m.filtered, 3) // web-prod-1, web-prod-2, web-dev
	assert.Equal(0, m.cursor)

	// 4. Press DOWN arrow key (KeyUp/Down has Text = "")
	// It should move the cursor down to index 1 (web-prod-2) without altering the filter text.
	sendKey(tea.Key{Code: tea.KeyDown})
	assert.Equal(1, m.cursor)
	assert.Equal("/w", string(m.filter))
	assert.True(m.search)

	// 5. Press UP arrow key
	// It should move the cursor back up to index 0 (web-prod-1)
	sendKey(tea.Key{Code: tea.KeyUp})
	assert.Equal(0, m.cursor)
	assert.Equal("/w", string(m.filter))
	assert.True(m.search)

	// 6. Press tab (should go down)
	sendKey(tea.Key{Code: tea.KeyTab})
	assert.Equal(1, m.cursor)

	// 7. Press shift+tab (should go up to 0)
	sendKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	assert.Equal(0, m.cursor)
	// Wait, let's verify if that works. If it does not, we'll see in the test run.
}
