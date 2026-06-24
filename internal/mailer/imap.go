package mailer

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPCreds describes how to authenticate to a receiving server. If OAuthToken
// is set, XOAUTH2 SASL is used; otherwise plain LOGIN with Password.
type IMAPCreds struct {
	Host       string
	Port       int
	Username   string
	Password   string
	OAuthToken string
}

// xoauth2Client implements the minimal sasl.Client interface for XOAUTH2, which
// go-sasl does not provide out of the box.
type xoauth2Client struct {
	username string
	token    string
}

func (x *xoauth2Client) Start() (mech string, ir []byte, err error) {
	resp := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", x.username, x.token)
	return "XOAUTH2", []byte(resp), nil
}

func (x *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// On failure the server sends a challenge; an empty response surfaces the error.
	return []byte(""), nil
}

// InboundMessage is the subset of an incoming email we care about for tracking.
type InboundMessage struct {
	UID        imap.UID
	MessageID  string
	InReplyTo  string
	References string
	FromAddr   string
	Subject    string
}

// isTransientNet reports whether a network error is worth retrying — notably the
// intermittent non-TLS responses some mail edges (e.g. Maildoso) return.
func isTransientNet(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, k := range []string{
		"does not look like a TLS handshake",
		"handshake failure",
		"broken pipe",
		"connection reset",
		"unexpected EOF",
		"i/o timeout",
		"connection refused",
		"EOF",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func dialIMAP(creds IMAPCreds, debug *bytes.Buffer) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", creds.Host, creds.Port)
	opts := &imapclient.Options{DebugWriter: debug}
	var c *imapclient.Client
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
		}
		c, err = imapclient.DialTLS(addr, opts)
		if err == nil || !isTransientNet(err) {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial: %w", err)
	}
	if creds.OAuthToken != "" {
		if err := c.Authenticate(&xoauth2Client{username: creds.Username, token: creds.OAuthToken}); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("imap xoauth2: %w", err)
		}
		return c, nil
	}
	if err := c.Login(creds.Username, creds.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}
	return c, nil
}

// annotateIMAP enriches a poll error with the host actually used and a redacted
// tail of the IMAP protocol transcript, so the failure reason is visible in the
// Accounts "Reply polling" cell instead of an opaque "unexpected EOF".
func annotateIMAP(err error, creds IMAPCreds, debug *bytes.Buffer) error {
	if err == nil {
		return nil
	}
	t := debug.String()
	for _, secret := range []string{creds.Password, creds.OAuthToken} {
		if secret != "" {
			t = strings.ReplaceAll(t, secret, "***")
		}
	}
	t = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(t)
	t = strings.TrimSpace(t)
	if len(t) > 400 {
		t = "…" + t[len(t)-400:]
	}
	if t == "" {
		return fmt.Errorf("%w [imap %s:%d]", err, creds.Host, creds.Port)
	}
	return fmt.Errorf("%w [imap %s:%d trace: %s]", err, creds.Host, creds.Port, t)
}

// VerifyIMAP dials and authenticates, validating credentials.
func VerifyIMAP(ctx context.Context, creds IMAPCreds) error {
	var debug bytes.Buffer
	c, err := dialIMAP(creds, &debug)
	if err != nil {
		return annotateIMAP(err, creds, &debug)
	}
	return c.Logout().Wait()
}

// NormalizeMessageID strips angle brackets/whitespace so message-ids compare
// consistently regardless of how the server formats them.
func NormalizeMessageID(s string) string {
	return strings.Trim(strings.TrimSpace(s), "<>")
}

var spamFolderNames = []string{"[Gmail]/Spam", "Junk", "Junk Email", "Spam", "Bulk Mail"}

// ScanSpamForWarmup looks in the account's spam/junk folder for warmup messages
// (isWarmup matches a normalized Message-ID), rescues them to the inbox (marking
// them seen so they aren't re-counted as inbox), and returns their Message-IDs so
// the caller can record them as spam placements. Best-effort: returns nil if no
// spam folder exists or it can't be read.
func ScanSpamForWarmup(ctx context.Context, creds IMAPCreds, isWarmup func(messageID string) bool) ([]string, error) {
	var debug bytes.Buffer
	c, err := dialIMAP(creds, &debug)
	if err != nil {
		return nil, annotateIMAP(err, creds, &debug)
	}
	defer func() { _ = c.Logout().Wait() }()

	var selected bool
	for _, name := range spamFolderNames {
		if _, err := c.Select(name, nil).Wait(); err == nil {
			selected = true
			break
		}
	}
	if !selected {
		return nil, nil // no spam folder on this provider
	}

	sd, err := c.UIDSearch(&imap.SearchCriteria{Since: time.Now().Add(-14 * 24 * time.Hour)}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("spam search: %w", err)
	}
	uids := sd.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{Envelope: true}).Collect()
	if err != nil {
		return nil, fmt.Errorf("spam fetch: %w", err)
	}

	var found []string
	var rescue []imap.UID
	for _, m := range msgs {
		if m.Envelope == nil {
			continue
		}
		mid := NormalizeMessageID(m.Envelope.MessageID)
		if mid != "" && isWarmup(mid) {
			found = append(found, mid)
			rescue = append(rescue, m.UID)
		}
	}
	if len(rescue) > 0 {
		set := imap.UIDSetNum(rescue...)
		// Mark seen first so the rescued copy isn't re-detected as an inbox landing.
		_ = c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close()
		if _, err := c.Move(set, "INBOX").Wait(); err != nil {
			// MOVE unsupported on some servers; the spam status is still recorded.
			_ = err
		}
	}
	return found, nil
}

// Poll fetches UNSEEN messages from INBOX, invokes handle for each, and marks
// successfully-handled messages as \Seen so they are not reprocessed.
// Poll reads INBOX messages whose UID is greater than sinceUID (the per-account
// watermark) and invokes handle for each, passing whether the message was already
// flagged Seen. It returns the highest UID it successfully handled so the caller
// can persist the new watermark. Reading by UID (not the UNSEEN flag) means a
// reply already opened in the mailbox is still detected. On the first run
// (sinceUID == 0) it bounds the backlog to the last 30 days and `max` messages.
func Poll(ctx context.Context, creds IMAPCreds, sinceUID uint32, max int, handle func(InboundMessage, bool) error) (uint32, error) {
	var debug bytes.Buffer
	c, err := dialIMAP(creds, &debug)
	if err != nil {
		return sinceUID, annotateIMAP(err, creds, &debug)
	}
	defer func() { _ = c.Logout().Wait() }()

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return sinceUID, annotateIMAP(fmt.Errorf("select inbox: %w", err), creds, &debug)
	}

	criteria := &imap.SearchCriteria{}
	if sinceUID == 0 {
		criteria.Since = time.Now().Add(-30 * 24 * time.Hour)
	} else {
		var set imap.UIDSet
		set.AddRange(imap.UID(sinceUID+1), 0) // sinceUID+1 .. * (0 means "no upper bound")
		criteria.UID = []imap.UIDSet{set}
	}
	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return sinceUID, fmt.Errorf("search: %w", err)
	}
	// Keep only UIDs strictly above the watermark, newest-first capped at max.
	var uids []imap.UID
	for _, u := range searchData.AllUIDs() {
		if uint32(u) > sinceUID {
			uids = append(uids, u)
		}
	}
	if len(uids) == 0 {
		return sinceUID, nil
	}
	if max > 0 && len(uids) > max {
		uids = uids[len(uids)-max:]
	}

	uidSet := imap.UIDSetNum(uids...)
	fetchOpts := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}},
		},
	}
	msgs, err := c.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return sinceUID, fmt.Errorf("fetch: %w", err)
	}

	high := sinceUID
	var handled []imap.UID
	for _, m := range msgs {
		in := InboundMessage{UID: m.UID}
		if m.Envelope != nil {
			in.MessageID = m.Envelope.MessageID
			in.InReplyTo = strings.Join(m.Envelope.InReplyTo, " ")
			in.Subject = m.Envelope.Subject
			if len(m.Envelope.From) > 0 {
				in.FromAddr = m.Envelope.From[0].Addr()
			}
		}
		for _, bs := range m.BodySection {
			in.References = parseReferences(string(bs.Bytes))
		}
		seen := false
		for _, f := range m.Flags {
			if f == imap.FlagSeen {
				seen = true
			}
		}
		if err := handle(in, seen); err == nil {
			handled = append(handled, m.UID)
			if uint32(m.UID) > high {
				high = uint32(m.UID)
			}
		}
	}

	// Mark handled messages Seen — this is the warmup "open" engagement signal.
	// Dedup no longer depends on it (the UID watermark does), so a reply read in
	// the mailbox first is still detected.
	if len(handled) > 0 {
		flags := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}
		if err := c.Store(imap.UIDSetNum(handled...), flags, nil).Close(); err != nil {
			return high, fmt.Errorf("mark seen: %w", err)
		}
	}
	return high, nil
}

// MarkImportant moves a message towards the primary inbox by flagging it (warmup).
func parseReferences(headerBlob string) string {
	for _, line := range strings.Split(headerBlob, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "references:") {
			return strings.TrimSpace(line[len("references:"):])
		}
	}
	return ""
}
