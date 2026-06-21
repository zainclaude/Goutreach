package mailer

import (
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

func dialIMAP(creds IMAPCreds) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", creds.Host, creds.Port)
	c, err := imapclient.DialTLS(addr, nil)
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

// VerifyIMAP dials and authenticates, validating credentials.
func VerifyIMAP(ctx context.Context, creds IMAPCreds) error {
	c, err := dialIMAP(creds)
	if err != nil {
		return err
	}
	return c.Logout().Wait()
}

// Poll fetches UNSEEN messages from INBOX, invokes handle for each, and marks
// successfully-handled messages as \Seen so they are not reprocessed.
func Poll(ctx context.Context, creds IMAPCreds, max int, handle func(InboundMessage) error) error {
	c, err := dialIMAP(creds)
	if err != nil {
		return err
	}
	defer c.Logout().Wait()

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return fmt.Errorf("select inbox: %w", err)
	}

	criteria := &imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
		Since:   time.Now().Add(-30 * 24 * time.Hour),
	}
	searchData, err := c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil
	}
	if max > 0 && len(uids) > max {
		uids = uids[len(uids)-max:]
	}

	uidSet := imap.UIDSetNum(uids...)
	fetchOpts := &imap.FetchOptions{
		Envelope: true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}},
		},
	}
	msgs, err := c.Fetch(uidSet, fetchOpts).Collect()
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

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
		if err := handle(in); err == nil {
			handled = append(handled, m.UID)
		}
	}

	if len(handled) > 0 {
		flags := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}}
		if err := c.Store(imap.UIDSetNum(handled...), flags, nil).Close(); err != nil {
			return fmt.Errorf("mark seen: %w", err)
		}
	}
	return nil
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
