package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// DefaultMaxFacts is the default cap on stored facts. It is used when
// MemoryConfig.MaxFacts is zero (i.e. unset in config).
const DefaultMaxFacts = 200

// Fact is one learned memory entry persisted to .spai/memory.jsonl.
type Fact struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	LearnedAt time.Time `json:"learned_at"`
}

// MemoryStore persists learned facts to <projectRoot>/.spai/memory.jsonl.
// It stores one Fact per line (JSONL). Each store operation is a
// read-modify-write to keep dedup and pruning consistent; the file is
// small (≤ DefaultMaxFacts lines) so this is not a performance concern.
type MemoryStore struct {
	path     string
	maxFacts int
}

// NewMemoryStore returns a MemoryStore rooted at the project containing
// workingDir (located by the same git-root walk as CheckpointStore).
// maxFacts ≤ 0 resolves to DefaultMaxFacts.
func NewMemoryStore(workingDir string, maxFacts int) *MemoryStore {
	if maxFacts <= 0 {
		maxFacts = DefaultMaxFacts
	}
	root := projectRoot(workingDir)
	return &MemoryStore{
		path:     filepath.Join(root, ".spai", "memory.jsonl"),
		maxFacts: maxFacts,
	}
}

// Load reads all stored facts from the JSONL file. It returns nil when the
// file does not exist. Corrupt lines are skipped with a log warning so a
// single bad entry never breaks the whole store.
func (m *MemoryStore) Load() ([]Fact, error) {
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var facts []Fact
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var f Fact
		if err := json.Unmarshal(line, &f); err != nil {
			log.Printf("session: memory: skipping corrupt entry: %v", err)
			continue
		}
		facts = append(facts, f)
	}
	return facts, sc.Err()
}

// Append adds a new fact or updates the value of an existing fact with the
// same key (preserving its position in the list). After the upsert the list
// is pruned so it never exceeds maxFacts, dropping the oldest entries first.
func (m *MemoryStore) Append(key, value string) error {
	facts, err := m.Load()
	if err != nil {
		return err
	}

	updated := false
	for i := range facts {
		if facts[i].Key == key {
			facts[i].Value = value
			facts[i].LearnedAt = time.Now().UTC()
			updated = true
			break
		}
	}
	if !updated {
		facts = append(facts, Fact{Key: key, Value: value, LearnedAt: time.Now().UTC()})
	}

	// Prune oldest entries when the cap is exceeded.
	if len(facts) > m.maxFacts {
		facts = facts[len(facts)-m.maxFacts:]
	}

	return m.save(facts)
}

// save writes facts to disk as JSONL, creating parent directories as needed.
func (m *MemoryStore) save(facts []Fact) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(m.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, fact := range facts {
		if err := enc.Encode(fact); err != nil {
			return err
		}
	}
	return nil
}
