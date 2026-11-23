package claudecode

import (
	"context"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

func (a *Adapter) Events(ctx context.Context, s agent.SessionRef, since string) (<-chan agent.Event, error) {
	return nil, agent.ErrUnsupported
}

func (a *Adapter) Start(ctx context.Context, r agent.StartReq) (agent.SessionRef, error) {
	return agent.SessionRef{}, agent.ErrUnsupported
}

func (a *Adapter) Resume(ctx context.Context, s agent.SessionRef, m *agent.Message) error {
	return agent.ErrUnsupported
}

func (a *Adapter) Send(ctx context.Context, s agent.SessionRef, m agent.Message) (agent.Delivery, error) {
	return agent.Delivery{}, agent.ErrUnsupported
}

func (a *Adapter) Fork(ctx context.Context, s agent.SessionRef, r agent.ForkReq) (agent.SessionRef, error) {
	return agent.SessionRef{}, agent.ErrUnsupported
}
