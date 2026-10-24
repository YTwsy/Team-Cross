package collab

import (
	"context"
	"path/filepath"
	"time"

	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func (a *App) memberAttention(spaceID, memberID string) (*shadow.AttentionStore, error) {
	a.attentionMu.Lock()
	defer a.attentionMu.Unlock()
	if a.shadowAttention == nil {
		a.shadowAttention = map[string]*shadow.AttentionStore{}
	}
	key := contentHash([]string{spaceID, memberID})
	if s := a.shadowAttention[key]; s != nil {
		return s, nil
	}
	s, err := shadow.OpenAttention(filepath.Join(a.Config.DataDir, "shadow-activity", key), spaceID, memberID)
	if err != nil {
		return nil, err
	}
	a.shadowAttention[key] = s
	return s, nil
}

func (a *App) pumpShadowAttention(ctx context.Context, spaceID string, store *shadow.AttentionStore) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := a.activityConsumerCall(ctx, spaceID, "ensure", ActivityConsumerInput{})
	if err != nil {
		return err
	}
	route := workbenchDecode[ActivityAgentRoute](out)
	if route.ShadowID == "" {
		return nil
	}
	r := shadow.AttentionRoute{ShadowID: route.ShadowID, Revision: route.Revision, Enabled: route.Enabled}
	if err := store.SetRoute(r); err != nil {
		return err
	}
	ack := func(receipt shadow.AttentionReceipt) error {
		if _, err := a.activityConsumerCall(ctx, spaceID, "ack", ActivityConsumerInput{ShadowID: receipt.Route.ShadowID, BaseRevision: receipt.Route.Revision, Receipt: receipt.Token}); err != nil {
			return err
		}
		return store.ConfirmReceipt(receipt)
	}
	if pending := store.PendingReceipt(); pending != nil {
		if err := ack(*pending); err != nil {
			return err
		}
	}
	if !route.Enabled {
		return store.SetError("")
	}
	out, err = a.activityConsumerCall(ctx, spaceID, "consume", ActivityConsumerInput{ShadowID: route.ShadowID, BaseRevision: route.Revision, Limit: 50})
	if err != nil {
		return err
	}
	page := workbenchDecode[ActivityConsumerPage](out)
	if page.Receipt == "" {
		return store.SetError("")
	}
	signals := make([]spacecontext.Signal, 0, len(page.Items))
	for _, item := range page.Items {
		signals = append(signals, spacecontext.Signal{ID: item.ID, Seq: item.Seq, Kind: item.Kind, Source: item.Source, Subject: item.Subject, Actor: item.Actor, CreatedAt: item.CreatedAt, Preview: item.Preview, Reasons: item.Reasons, Follow: item.Follow})
	}
	receipt := shadow.AttentionReceipt{Token: page.Receipt, Route: r, Through: page.ThroughSeq, Initial: page.Initial}
	if err := store.Accept(receipt, signals); err != nil {
		return err
	}
	return ack(receipt)
}
