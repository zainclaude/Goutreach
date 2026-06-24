// Package warmup runs a peer-network email warmup across the user's own inboxes,
// ramping daily volume so mailboxes build sending reputation and stay out of spam.
package warmup

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

// Service sends warmup mail between connected inboxes.
type Service struct {
	st  *store.Store
	res *mailauth.Resolver
	log *log.Logger
}

// New builds the warmup Service.
func New(st *store.Store, res *mailauth.Resolver, logger *log.Logger) *Service {
	return &Service{st: st, res: res, log: logger}
}

// Run sends warmup mail on an hourly cadence until the context is cancelled.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	accounts, err := s.st.ListWarmupAccounts(ctx)
	if err != nil {
		s.log.Printf("warmup: list: %v", err)
		return
	}
	if len(accounts) < 2 {
		return // need at least two inboxes to exchange warmup mail
	}
	for _, acc := range accounts {
		if err := s.warmAccount(ctx, acc, accounts); err != nil {
			s.log.Printf("warmup: account %d: %v", acc.ID, err)
		}
	}
}

func (s *Service) warmAccount(ctx context.Context, acc store.EmailAccount, all []store.EmailAccount) error {
	now := time.Now()
	// Anchor the ramp on the first warmup send so a long-idle inbox still eases
	// in from 2/day. Before any send (and on the first day), the anchor is now.
	anchor := now
	if first, ok, err := s.st.FirstWarmupSentAt(ctx, acc.ID); err != nil {
		return err
	} else if ok {
		anchor = first
	}
	target := rampTarget(acc, anchor, now)
	sent, err := s.st.CountWarmupSentToday(ctx, acc.ID)
	if err != nil {
		return err
	}
	remaining := target - sent
	days := int(now.Sub(anchor).Hours() / 24)
	s.log.Printf("warmup: %s — target today=%d (day %d since first send), sent today=%d, remaining=%d, cap=%d",
		acc.Email, target, days, sent, remaining, acc.WarmupTargetPerDay)
	if remaining <= 0 {
		return nil
	}
	// Spread volume across ticks: at most 2 per half-hour tick.
	batch := remaining
	if batch > 2 {
		batch = 2
	}

	creds, err := s.res.SMTP(ctx, acc)
	if err != nil {
		return err
	}

	for i := 0; i < batch; i++ {
		recipient := pickRecipient(acc, all)
		if recipient.ID == 0 {
			return nil
		}
		subject, body := compose()
		mid, err := mailer.Send(ctx, creds, mailer.OutgoingEmail{
			FromAddr: acc.Email,
			FromName: acc.FromName,
			ToAddr:   recipient.Email,
			Subject:  subject,
			TextBody: body,
		})
		if err != nil {
			return fmt.Errorf("send warmup: %w", err)
		}
		if err := s.st.RecordWarmupSent(ctx, acc.ID, recipient.ID, subject, mid); err != nil {
			return err
		}
		time.Sleep(time.Duration(1+rand.Intn(3)) * time.Second)
	}
	return nil
}

// rampTarget grows the daily warmup volume with the number of days since the
// account's first warmup send (anchor), capped at the account's configured
// target. Day 0 = 2/day, then +3/day (2, 5, 8, …) until the cap.
func rampTarget(acc store.EmailAccount, anchor, now time.Time) int {
	// An unset/zero per-account cap means "use the default" — otherwise the cap
	// below would pin the ramp at the floor (2/day) forever and it'd never grow.
	maxPerDay := acc.WarmupTargetPerDay
	if maxPerDay <= 0 {
		maxPerDay = 20
	}
	days := int(now.Sub(anchor).Hours() / 24)
	if days < 0 {
		days = 0
	}
	target := 2 + days*3
	if target > maxPerDay {
		target = maxPerDay
	}
	if target < 2 {
		target = 2
	}
	return target
}

func pickRecipient(self store.EmailAccount, all []store.EmailAccount) store.EmailAccount {
	var pool []store.EmailAccount
	for _, a := range all {
		if a.ID != self.ID {
			pool = append(pool, a)
		}
	}
	if len(pool) == 0 {
		return store.EmailAccount{}
	}
	return pool[rand.Intn(len(pool))]
}

var subjects = []string{
	"Quick question", "Following up", "Re: our chat", "Coffee next week?",
	"Project update", "Thoughts?", "Hello", "Checking in", "Notes from today",
}

var bodies = []string{
	"Hey — just wanted to touch base. Hope things are going well on your end. Talk soon.",
	"Thanks again for the help earlier. Let me know if you need anything from me.",
	"Got a minute this week to sync? No rush, whenever works.",
	"Sharing a quick note so we stay aligned. Appreciate you!",
	"Hope you're having a good week. Let's catch up soon.",
}

func compose() (string, string) {
	return subjects[rand.Intn(len(subjects))], bodies[rand.Intn(len(bodies))]
}
