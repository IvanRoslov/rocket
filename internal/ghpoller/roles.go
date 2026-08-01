package ghpoller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// maxPayloadBody caps how much of an issue/comment body is copied into an
// inbox payload. The role reads the full text from GitHub itself when it
// needs to; the inbox only has to carry enough for triage.
const maxPayloadBody = 4096

// truncationMarker is appended to a body cut at maxPayloadBody.
const truncationMarker = "…[truncated]"

// seenRetention is how long dedup rows are kept. The watermark only ever
// moves forward, so an issue/comment older than this can no longer be a
// candidate and its dedup row is dead weight.
const seenRetention = 30 * 24 * time.Hour

// roleMarkerRE matches the invisible trailer a role appends to every GitHub
// write it makes (`<!-- rocket-agent:sre -->`). Any body carrying such a
// marker — of this role or another one — is skipped: it is a role's own
// writing, and enqueueing it would either loop the role on itself or bounce
// two roles off each other. Authorship cannot be used for this, because the
// daemon token and the rocket owner are the same GitHub account.
var roleMarkerRE = regexp.MustCompile(`(?i)<!--\s*rocket-agent:[a-z0-9-]+\s*-->`)

// issuePayload is the JSON payload of an issue_opened / issue_comment inbox
// event. CommentID and the comment-specific fields are omitted for
// issue_opened.
type issuePayload struct {
	Repo      string   `json:"repo"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Body      string   `json:"body"`
	HTMLURL   string   `json:"html_url"`
	Labels    []string `json:"labels"`
	CommentID int64    `json:"comment_id,omitempty"`
}

// tickRoles polls the GitHub subscriptions of every enabled role and turns
// new issues and comments into inbox events. It is called from Tick, so it
// shares the PR poller's interval, client (with its ETag cache) and backoff
// behaviour.
func (p *Poller) tickRoles(ctx context.Context, client *github.Client) error {
	roles, err := p.st.ListAgents("")
	if err != nil {
		return fmt.Errorf("list roles: %w", err)
	}

	for _, role := range roles {
		if !role.Enabled {
			continue
		}
		for _, sub := range role.Subscriptions {
			owner, repo, ok := splitRepo(sub.Repo)
			if !ok {
				slog.Warn("ghpoller: role subscription is not owner/repo",
					"role", role.ID, "repo", sub.Repo)
				continue
			}
			err := p.pollSubscription(ctx, client, role, sub, owner, repo)
			if errors.Is(err, github.ErrBackoff) {
				// Rate limited: give up on the whole role pass, the caller
				// aborts the tick.
				return err
			}
			if errors.Is(err, github.ErrForbidden) {
				p.warnPermissionOnce(sub.Repo, "issues")
				continue
			}
			if err != nil {
				slog.Error("ghpoller: role subscription", "role", role.ID,
					"repo", sub.Repo, "error", err)
			}
		}
	}
	return nil
}

// pollSubscription polls one repository for one role.
func (p *Poller) pollSubscription(ctx context.Context, client *github.Client,
	role store.Agent, sub store.AgentSubscription, owner, repo string) error {

	watermark, err := p.st.AgentGHWatermark(role.ID, sub.Repo)
	if err != nil {
		return err
	}
	if watermark == 0 {
		return p.seedSubscription(ctx, client, role, sub, owner, repo)
	}

	since := time.Unix(watermark, 0)
	newest := watermark

	issues, err := client.ListIssuesSince(ctx, owner, repo, "open", since)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		created := parseGHTime(issue.CreatedAt)
		if created > newest {
			newest = created
		}
		if created < watermark || !p.issueMatches(role, sub, issue) {
			continue
		}
		fresh, err := p.st.MarkAgentGHSeen(role.ID, sub.Repo, store.GHSeenIssue, int64(issue.Number))
		if err != nil {
			return err
		}
		if !fresh {
			continue
		}
		if err := p.enqueue(role.ID, "issue_opened", issuePayload{
			Repo:    sub.Repo,
			Number:  issue.Number,
			Title:   issue.Title,
			Author:  issue.User.Login,
			Body:    truncateBody(issue.Body),
			HTMLURL: issue.HTMLURL,
			Labels:  labelNames(issue),
		}); err != nil {
			return err
		}
	}

	comments, err := client.ListIssueCommentsSince(ctx, owner, repo, since)
	if err != nil {
		return err
	}
	for _, cm := range comments {
		created := parseGHTime(cm.CreatedAt)
		if created > newest {
			newest = created
		}
		if created < watermark {
			continue
		}
		relevant, err := p.commentMatches(role, sub, cm)
		if err != nil {
			return err
		}
		if !relevant {
			continue
		}
		fresh, err := p.st.MarkAgentGHSeen(role.ID, sub.Repo, store.GHSeenComment, cm.ID)
		if err != nil {
			return err
		}
		if !fresh {
			continue
		}
		if err := p.enqueue(role.ID, "issue_comment", issuePayload{
			Repo:      sub.Repo,
			Number:    cm.IssueNumber,
			Author:    cm.User.Login,
			Body:      truncateBody(cm.Body),
			HTMLURL:   cm.HTMLURL,
			CommentID: cm.ID,
		}); err != nil {
			return err
		}
	}

	if newest > watermark {
		if err := p.st.SetAgentGHWatermark(role.ID, sub.Repo, newest); err != nil {
			return err
		}
	}
	return p.st.PruneAgentGHSeen(time.Now().Add(-seenRetention).Unix())
}

// seedSubscription records the state of a freshly subscribed repository
// without enqueueing anything: the role starts from "now", so subscribing to
// a busy repository does not dump its whole open-issue backlog into the
// inbox. Triage of the existing backlog stays reachable through the role's
// own cron policy (`gh issue list` from inside an instance).
func (p *Poller) seedSubscription(ctx context.Context, client *github.Client,
	role store.Agent, sub store.AgentSubscription, owner, repo string) error {

	issues, err := client.ListIssuesSince(ctx, owner, repo, "open", time.Time{})
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if _, err := p.st.MarkAgentGHSeen(role.ID, sub.Repo, store.GHSeenIssue, int64(issue.Number)); err != nil {
			return err
		}
	}
	return p.st.SetAgentGHWatermark(role.ID, sub.Repo, time.Now().Unix())
}

// issueMatches applies the subscription filters to a newly seen issue: the
// label filter matches when the issue carries any of the configured labels,
// and mention_only requires the role to be mentioned in the title or body.
// Both filters are ANDed when both are configured.
func (p *Poller) issueMatches(role store.Agent, sub store.AgentSubscription, issue github.Issue) bool {
	if hasRoleMarker(issue.Body) {
		return false
	}
	if len(sub.Labels) > 0 && !anyLabel(issue, sub.Labels) {
		return false
	}
	if sub.MentionOnly && !mentions(issue.Title+"\n"+issue.Body, role.ID) {
		return false
	}
	return true
}

// commentMatches keeps a comment when the issue it belongs to is in the
// role's dossier, or when the comment mentions the role. The subscription
// label filter deliberately does not apply here: once an issue is in the
// dossier the role tracks it regardless of how its labels evolve.
func (p *Poller) commentMatches(role store.Agent, sub store.AgentSubscription, cm github.IssueComment) (bool, error) {
	if hasRoleMarker(cm.Body) {
		return false, nil
	}
	if mentions(cm.Body, role.ID) {
		return true, nil
	}

	ref := fmt.Sprintf("%s#%d", sub.Repo, cm.IssueNumber)
	_, err := p.st.GetAgentItem(role.ID, "issue", ref)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// enqueue appends one inbox event with a JSON payload.
func (p *Poller) enqueue(roleID, kind string, payload issuePayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	if _, err := p.st.EnqueueInboxEvent(store.AgentInboxEvent{
		RoleID:  roleID,
		Kind:    kind,
		Payload: string(raw),
	}); err != nil {
		return err
	}
	p.bus.Publish("agent."+kind, roleID, map[string]any{
		"role":   roleID,
		"repo":   payload.Repo,
		"number": payload.Number,
	})
	return nil
}

func labelNames(issue github.Issue) []string {
	names := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		names = append(names, l.Name)
	}
	return names
}

func anyLabel(issue github.Issue, want []string) bool {
	for _, l := range issue.Labels {
		for _, w := range want {
			if strings.EqualFold(l.Name, w) {
				return true
			}
		}
	}
	return false
}

// mentions reports whether text contains an @<roleID> mention. The character
// after the id must not continue an identifier, so "@sreteam" does not count
// as a mention of "sre".
func mentions(text, roleID string) bool {
	lower := strings.ToLower(text)
	needle := "@" + strings.ToLower(roleID)
	for i := 0; ; {
		idx := strings.Index(lower[i:], needle)
		if idx < 0 {
			return false
		}
		end := i + idx + len(needle)
		if end >= len(lower) || !isMentionChar(lower[end]) {
			return true
		}
		i = end
	}
}

func isMentionChar(c byte) bool {
	return c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func hasRoleMarker(body string) bool {
	return roleMarkerRE.MatchString(body)
}

// truncateBody caps a body at maxPayloadBody bytes, cutting on a rune
// boundary and marking the cut.
func truncateBody(body string) string {
	if len(body) <= maxPayloadBody {
		return body
	}
	cut := body[:maxPayloadBody]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + truncationMarker
}

// splitRepo splits "owner/repo" into its parts.
func splitRepo(full string) (owner, repo string, ok bool) {
	owner, repo, ok = strings.Cut(full, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", false
	}
	return owner, repo, true
}

// parseGHTime parses a GitHub RFC3339 timestamp into unix seconds; an
// unparseable value yields 0, which makes the caller treat the object as
// older than any watermark (i.e. skip it) rather than enqueue garbage.
func parseGHTime(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.Unix()
}
