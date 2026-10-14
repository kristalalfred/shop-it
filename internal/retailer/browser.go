package retailer

import (
	"context"
	"crypto/rand"
	"slices"
	"sync"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type Session struct {
	RetailerID string                `json:"retailer_id"`
	ClientID   string                `json:"client_id"`
	Context    shopping.StoreContext `json:"context"`
	Name       string                `json:"name"`
	LoggedIn   bool                  `json:"logged_in"`
	SeenAt     time.Time             `json:"seen_at"`
}

type SearchJob struct {
	ID string `json:"id"`
	shopping.SearchRequest
}

type SearchReply struct {
	Result *shopping.SearchResult `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

type pendingSearch struct {
	job      SearchJob
	clientID string
	claimed  bool
	reply    chan SearchReply
}

type Bridge struct {
	mu       sync.Mutex
	sessions map[string][]Session
	jobs     []*pendingSearch
	timeout  time.Duration
}

func NewBridge(timeout time.Duration) *Bridge {
	return &Bridge{sessions: map[string][]Session{}, timeout: timeout}
}

func (b *Bridge) Publish(clientID string, sessions []Session) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sessions = slices.Clone(sessions)
	for i := range sessions {
		sessions[i].ClientID = clientID
		sessions[i].SeenAt = time.Now().UTC()
		sessions[i].Context.Memberships = slices.Clone(sessions[i].Context.Memberships)
	}
	b.sessions[clientID] = sessions
}

func (b *Bridge) Sessions() []Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessionsLocked()
}

func (b *Bridge) sessionsLocked() []Session {
	result := []Session{}
	for _, sessions := range b.sessions {
		for _, session := range sessions {
			if time.Since(session.SeenAt) < 45*time.Second {
				session.Context.Memberships = slices.Clone(session.Context.Memberships)
				result = append(result, session)
			}
		}
	}
	slices.SortFunc(result, func(a, c Session) int {
		if a.RetailerID < c.RetailerID || a.RetailerID == c.RetailerID && a.ClientID < c.ClientID {
			return -1
		}
		if a.RetailerID == c.RetailerID && a.ClientID == c.ClientID {
			return 0
		}
		return 1
	})
	return result
}

func (b *Bridge) HasSession(retailerID, clientID string, context shopping.StoreContext, requireLogin bool) bool {
	for _, session := range b.Sessions() {
		if session.RetailerID == retailerID && session.ClientID == clientID && SameContext(session.Context, context) && (!requireLogin || session.LoggedIn) {
			return true
		}
	}
	return false
}

func (b *Bridge) Search(ctx context.Context, request shopping.SearchRequest) (shopping.SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	b.mu.Lock()
	clientID := ""
	for _, session := range b.sessionsLocked() {
		if session.RetailerID == request.RetailerID && SameContext(session.Context, request.Context) {
			clientID = session.ClientID
			break
		}
	}
	if clientID == "" || len(b.jobs) >= 100 {
		b.mu.Unlock()
		return shopping.SearchResult{}, shopping.ErrUpstream
	}
	request.Context.Memberships = slices.Clone(request.Context.Memberships)
	job := &pendingSearch{job: SearchJob{ID: "search-" + rand.Text(), SearchRequest: request}, clientID: clientID, reply: make(chan SearchReply, 1)}
	b.jobs = append(b.jobs, job)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.jobs = slices.DeleteFunc(b.jobs, func(other *pendingSearch) bool { return other == job })
		b.mu.Unlock()
	}()
	select {
	case reply := <-job.reply:
		if reply.Error != "" || reply.Result == nil {
			return shopping.SearchResult{}, shopping.ErrUpstream
		}
		return *reply.Result, ValidateResult(request, *reply.Result)
	case <-ctx.Done():
		return shopping.SearchResult{}, shopping.ErrTimeout
	}
}

func (b *Bridge) Claim(clientID string) []SearchJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	jobs := []SearchJob{}
	for _, job := range b.jobs {
		if job.clientID == clientID && !job.claimed && len(jobs) < 6 {
			job.claimed = true
			copy := job.job
			copy.Context.Memberships = slices.Clone(copy.Context.Memberships)
			jobs = append(jobs, copy)
		}
	}
	return jobs
}

func (b *Bridge) Complete(clientID, id string, reply SearchReply) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, job := range b.jobs {
		if job.job.ID == id && job.clientID == clientID && job.claimed {
			if reply.Result != nil && reply.Error == "" {
				if err := ValidateResult(job.job.SearchRequest, *reply.Result); err != nil {
					return shopping.ErrInvalid
				}
			} else if reply.Result != nil || reply.Error != "adapter unavailable" {
				return shopping.ErrInvalid
			}
			select {
			case job.reply <- reply:
				return nil
			default:
				return shopping.ErrConflict
			}
		}
	}
	return shopping.ErrNotFound
}
