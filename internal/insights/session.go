package insights

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Thought captures a single planning step submitted by the client.
type Thought struct {
	Thought           string
	ThoughtNumber     int
	TotalThoughts     int
	NextThoughtNeeded bool

	IsRevision        bool
	RevisesThought    int
	BranchFromThought int
	BranchID          string
	NeedsMoreThoughts bool
}

// Session holds a short history of thoughts and optional branches.
type Session struct {
	ID        string
	Thoughts  []Thought
	Branches  map[string][]Thought
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionStore is an in-memory store for sessions. It is not persisted and
// is intended for short-lived use. It is safe for concurrent access.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	maxKeep  int           // max thoughts to keep per session
	ttl      time.Duration // future: optional TTL management
}

func NewSessionStore(maxKeep int) *SessionStore {
	if maxKeep <= 0 {
		maxKeep = 20
	}
	return &SessionStore{
		sessions: make(map[string]*Session),
		maxKeep:  maxKeep,
		ttl:      0,
	}
}

func (s *SessionStore) NewSession() *Session {
	id := randomID()
	sess := &Session{ID: id, Thoughts: []Thought{}, Branches: map[string][]Thought{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	return sess, ok
}

func (s *SessionStore) Reset(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := &Session{ID: id, Thoughts: []Thought{}, Branches: map[string][]Thought{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	s.sessions[id] = sess
	return sess
}

func (s *SessionStore) AppendThought(sess *Session, t Thought) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.UpdatedAt = time.Now()
	sess.Thoughts = append(sess.Thoughts, t)
	if len(sess.Thoughts) > s.maxKeep {
		// keep only the last maxKeep thoughts
		sess.Thoughts = sess.Thoughts[len(sess.Thoughts)-s.maxKeep:]
	}
	if t.BranchFromThought > 0 && t.BranchID != "" {
		if sess.Branches == nil {
			sess.Branches = map[string][]Thought{}
		}
		sess.Branches[t.BranchID] = append(sess.Branches[t.BranchID], t)
	}
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
