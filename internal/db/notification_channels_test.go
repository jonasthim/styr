package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestChannel(name string, events []string) domain.NotificationChannel {
	return domain.NotificationChannel{
		ID:        uuid.NewString(),
		Kind:      domain.ChannelNtfy,
		Name:      name,
		URL:       "https://ntfy.sh/styr-" + name,
		Events:    events,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
}

func TestNotificationChannels_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	channels := NewNotificationChannels(testOpenDB(t))

	c := newTestChannel("ops", []string{"run.finished", "run.failed"})
	c.TokenCiphertext = []byte{1, 2, 3}
	c.TokenNonce = []byte{4, 5, 6}
	if err := channels.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := channels.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != c.Name || got.Kind != domain.ChannelNtfy || !got.Enabled {
		t.Fatalf("Get = %+v, want matching %+v", got, c)
	}
	if len(got.Events) != 2 || got.Events[0] != "run.finished" {
		t.Fatalf("Get Events = %v, want [run.finished run.failed]", got.Events)
	}
	if string(got.TokenCiphertext) != string(c.TokenCiphertext) || string(got.TokenNonce) != string(c.TokenNonce) {
		t.Fatalf("Get token bytes did not round-trip: got ciphertext=%v nonce=%v", got.TokenCiphertext, got.TokenNonce)
	}

	c.Name = "ops-renamed"
	c.Enabled = false
	if err := channels.Update(ctx, c); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = channels.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "ops-renamed" || got.Enabled {
		t.Fatalf("Get after update = %+v", got)
	}

	if err := channels.Delete(ctx, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := channels.Get(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestNotificationChannels_CreateWithoutTokenLeavesNilBytes(t *testing.T) {
	ctx := context.Background()
	channels := NewNotificationChannels(testOpenDB(t))

	c := newTestChannel("no-token", []string{"run.finished"})
	if err := channels.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := channels.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TokenCiphertext != nil || got.TokenNonce != nil {
		t.Fatalf("Get without token = ciphertext=%v nonce=%v, want nil", got.TokenCiphertext, got.TokenNonce)
	}
}

func TestNotificationChannels_List(t *testing.T) {
	ctx := context.Background()
	channels := NewNotificationChannels(testOpenDB(t))

	for _, name := range []string{"b-channel", "a-channel"} {
		if err := channels.Create(ctx, newTestChannel(name, []string{"run.finished"})); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	list, err := channels.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list[0].Name != "a-channel" || list[1].Name != "b-channel" {
		t.Fatalf("List not ordered by name: %+v", list)
	}
}

func TestNotificationChannels_ListEnabledFor(t *testing.T) {
	ctx := context.Background()
	channels := NewNotificationChannels(testOpenDB(t))

	finishedOnly := newTestChannel("finished-only", []string{"run.finished"})
	failedAndHuman := newTestChannel("failed-and-human", []string{"run.failed", "run.needs_human"})
	disabled := newTestChannel("disabled", []string{"run.finished"})
	disabled.Enabled = false
	for _, c := range []domain.NotificationChannel{finishedOnly, failedAndHuman, disabled} {
		if err := channels.Create(ctx, c); err != nil {
			t.Fatalf("Create %s: %v", c.Name, err)
		}
	}

	forFinished, err := channels.ListEnabledFor(ctx, "run.finished")
	if err != nil {
		t.Fatalf("ListEnabledFor run.finished: %v", err)
	}
	if len(forFinished) != 1 || forFinished[0].Name != "finished-only" {
		t.Fatalf("ListEnabledFor run.finished = %+v, want only finished-only", forFinished)
	}

	forFailed, err := channels.ListEnabledFor(ctx, "run.failed")
	if err != nil {
		t.Fatalf("ListEnabledFor run.failed: %v", err)
	}
	if len(forFailed) != 1 || forFailed[0].Name != "failed-and-human" {
		t.Fatalf("ListEnabledFor run.failed = %+v, want only failed-and-human", forFailed)
	}

	forUnknown, err := channels.ListEnabledFor(ctx, "run.does_not_exist")
	if err != nil {
		t.Fatalf("ListEnabledFor unknown event: %v", err)
	}
	if len(forUnknown) != 0 {
		t.Fatalf("ListEnabledFor unknown event = %+v, want none", forUnknown)
	}
}
