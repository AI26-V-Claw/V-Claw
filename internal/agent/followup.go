package agent

import "context"

// FollowUpMessage is a best-effort outbound message emitted after the main
// response has already been sent, for short-lived checks such as Gmail bounces.
type FollowUpMessage struct {
	SessionID string
	Text      string
}

type FollowUpSink func(context.Context, FollowUpMessage)

type followUpSinkKey struct{}

func WithFollowUpSink(ctx context.Context, sink FollowUpSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, followUpSinkKey{}, sink)
}

func followUpSinkFromContext(ctx context.Context) (FollowUpSink, bool) {
	sink, ok := ctx.Value(followUpSinkKey{}).(FollowUpSink)
	return sink, ok && sink != nil
}
