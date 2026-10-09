package scheduler

import (
	"sync"
	"time"

	"github.com/rm4n0s/bos/internal/command"
	"github.com/rm4n0s/bos/internal/domain"
)

// targetCache remembers which servers each command names as its targets
// (SPEC §6.9), read from the stored parsed form. An entry is read again when the
// command is edited.
type targetCache struct {
	mu      sync.Mutex
	entries map[domain.ID]targetEntry
}

type targetEntry struct {
	updated time.Time
	codes   map[string]bool
}

func newTargetCache() *targetCache { return &targetCache{entries: map[domain.ID]targetEntry{}} }

// refresh updates the cache from the commands that are waiting or running and
// forgets the others.
func (c *targetCache) refresh(cmds []domain.Command) {
	c.mu.Lock()
	defer c.mu.Unlock()
	live := make(map[domain.ID]bool, len(cmds))
	for i := range cmds {
		cmd := &cmds[i]
		live[cmd.ID] = true
		if e, ok := c.entries[cmd.ID]; ok && e.updated.Equal(cmd.UpdatedAt) {
			continue
		}
		e := targetEntry{updated: cmd.UpdatedAt, codes: map[string]bool{}}
		if parsed, err := command.FromJSON(cmd.CommandJSON); err == nil {
			for _, t := range parsed.Servers {
				e.codes[t.Server.Text] = true
			}
		}
		c.entries[cmd.ID] = e
	}
	for id := range c.entries {
		if !live[id] {
			delete(c.entries, id)
		}
	}
}

// has reports whether the server is a target of the command.
func (c *targetCache) has(cmd domain.ID, serverCode string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[cmd].codes[serverCode]
}

// commandsFor returns the ids of the commands the server is a target of.
func (c *targetCache) commandsFor(serverCode string) []domain.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]domain.ID, 0, len(c.entries)+1)
	for id, e := range c.entries {
		if e.codes[serverCode] {
			ids = append(ids, id)
		}
	}
	return append(ids, domain.ReplID)
}
